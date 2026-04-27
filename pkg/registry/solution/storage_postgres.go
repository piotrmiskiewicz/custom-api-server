package solution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS solutions (
    namespace            varchar(253) NOT NULL,
    name                 varchar(253) NOT NULL,
    uid                  varchar(64) NOT NULL,
    resource_version     INTEGER NOT NULL DEFAULT 1,
    creation_timestamp   TIMESTAMPTZ NOT NULL,
    labels               JSONB NOT NULL DEFAULT '{}',
    spec_solution_name   TEXT NOT NULL DEFAULT '',
    status_phase         TEXT NOT NULL DEFAULT '',
    status_conditions    JSONB NOT NULL DEFAULT '[]',
    PRIMARY KEY (namespace, name)
)`

// PostgresStorage is a PostgreSQL-backed REST storage for Solution objects.
type PostgresStorage struct {
	db             *pgxpool.Pool
	dsn            string
	watchMu        sync.Mutex
	watchers       []*watcher
	listenerCancel context.CancelFunc
}

// NewPostgresStorage connects to the given DSN, creates the schema if needed,
// and returns a ready-to-use PostgresStorage.
func NewPostgresStorage(ctx context.Context, dsn string) (*PostgresStorage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if _, err := pool.Exec(ctx, createTableSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	lctx, cancel := context.WithCancel(context.Background())
	s := &PostgresStorage{
		db:             pool,
		dsn:            dsn,
		listenerCancel: cancel,
	}
	go s.listenLoop(lctx)
	return s, nil
}

// broadcast sends an event to all active watchers and removes stopped ones.
func (s *PostgresStorage) broadcast(eventType watch.EventType, obj *internal.Solution) {
	s.watchMu.Lock()
	alive := s.watchers[:0]
	for _, w := range s.watchers {
		if w.send(eventType, obj) {
			alive = append(alive, w)
		}
	}
	for i := len(alive); i < len(s.watchers); i++ {
		s.watchers[i] = nil
	}
	s.watchers = alive
	s.watchMu.Unlock()
}

type notifyPayload struct {
	Type              string          `json:"type"`
	Namespace         string          `json:"namespace"`
	Name              string          `json:"name"`
	UID               string          `json:"uid"`
	ResourceVersion   int             `json:"resource_version"`
	CreationTimestamp time.Time       `json:"creation_timestamp"`
	Labels            json.RawMessage `json:"labels"`
	SpecSolutionName  string          `json:"spec_solution_name"`
	StatusPhase       string          `json:"status_phase"`
	StatusConditions  json.RawMessage `json:"status_conditions"`
}

func (s *PostgresStorage) notifyPayloadFor(eventType string, sol *internal.Solution) (string, error) {
	labelsJSON, err := json.Marshal(sol.Labels)
	if err != nil {
		return "", err
	}
	conditionsJSON, err := json.Marshal(sol.Status.Conditions)
	if err != nil {
		return "", err
	}
	rv, _ := strconv.Atoi(sol.ResourceVersion)
	p := notifyPayload{
		Type:              eventType,
		Namespace:         sol.Namespace,
		Name:              sol.Name,
		UID:               string(sol.UID),
		ResourceVersion:   rv,
		CreationTimestamp: sol.CreationTimestamp.Time,
		Labels:            labelsJSON,
		SpecSolutionName:  sol.Spec.SolutionName,
		StatusPhase:       string(sol.Status.Phase),
		StatusConditions:  conditionsJSON,
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func payloadToSolution(p *notifyPayload) (*internal.Solution, error) {
	return buildSolution(
		p.Namespace, p.Name, p.UID, p.ResourceVersion,
		p.CreationTimestamp, p.Labels,
		p.SpecSolutionName, p.StatusPhase, p.StatusConditions,
	)
}

// listenLoop runs for the lifetime of the storage, listening for NOTIFY on the
// "solutions" channel and broadcasting events to registered watchers.
// It retries on connection loss with exponential backoff.
func (s *PostgresStorage) listenLoop(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 16 * time.Second
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if err := s.listenOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		return
	}
	fmt.Fprintf(os.Stderr, "postgres watch: listener failed after %d attempts, no more watch events\n", maxAttempts)
}

// listenOnce opens a dedicated pgconn connection, runs LISTEN, and loops on
// WaitForNotification until the context is cancelled or an error occurs.
func (s *PostgresStorage) listenOnce(ctx context.Context) error {
	conn, err := pgconn.Connect(ctx, s.dsn)
	if err != nil {
		return fmt.Errorf("pgconn.Connect: %w", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN solutions").ReadAll(); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return err // connection lost
		}

		var p notifyPayload
		if err := json.Unmarshal([]byte(n.Payload), &p); err != nil {
			continue
		}
		sol, err := payloadToSolution(&p)
		if err != nil {
			continue
		}

		var eventType watch.EventType
		switch p.Type {
		case "ADDED":
			eventType = watch.Added
		case "MODIFIED":
			eventType = watch.Modified
		case "DELETED":
			eventType = watch.Deleted
		default:
			continue
		}
		s.broadcast(eventType, sol)
	}
}

// --- rest.Scoper ---

func (s *PostgresStorage) NamespaceScoped() bool { return true }

// --- rest.SingularNameProvider ---

func (s *PostgresStorage) GetSingularName() string { return "solution" }

// --- rest.StandardStorage ---

func (s *PostgresStorage) New() runtime.Object { return &internal.Solution{} }

func (s *PostgresStorage) NewList() runtime.Object { return &internal.SolutionList{} }

func (s *PostgresStorage) Destroy() {
	s.listenerCancel()
	s.watchMu.Lock()
	for _, w := range s.watchers {
		w.Stop()
	}
	s.watchers = nil
	s.watchMu.Unlock()
	s.db.Close()
}

func (s *PostgresStorage) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	row := s.db.QueryRow(ctx,
		`SELECT uid, resource_version, creation_timestamp, labels,
		        spec_solution_name, status_phase, status_conditions
		 FROM solutions WHERE namespace=$1 AND name=$2`,
		ns, name)

	sol, err := scanSolution(ns, name, row)
	if err != nil {
		return nil, errors.NewNotFound(solutionGR, name)
	}
	return sol, nil
}

func (s *PostgresStorage) List(ctx context.Context, opts *metainternalversion.ListOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)

	var fieldSel fields.Selector
	if opts != nil && opts.FieldSelector != nil {
		if err := validateFieldSelector(opts.FieldSelector); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		fieldSel = opts.FieldSelector
	}

	// Build query — optionally filter by spec.solutionName at DB level.
	query := `SELECT uid, resource_version, creation_timestamp, labels,
	                 spec_solution_name, status_phase, status_conditions, name
	          FROM solutions WHERE namespace=$1`
	args := []any{ns}

	if fieldSel != nil && !fieldSel.Empty() {
		for _, req := range fieldSel.Requirements() {
			if req.Field == "spec.solutionName" {
				query += " AND spec_solution_name=$2"
				args = append(args, req.Value)
			}
		}
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list solutions: %w", err)
	}
	defer rows.Close()

	list := &internal.SolutionList{}
	for rows.Next() {
		var (
			uid, statusPhase, specSolutionName, name string
			rv                                       int
			createdAt                                time.Time
			labelsJSON, conditionsJSON               []byte
		)
		if err := rows.Scan(&uid, &rv, &createdAt, &labelsJSON, &specSolutionName, &statusPhase, &conditionsJSON, &name); err != nil {
			return nil, fmt.Errorf("scan solution row: %w", err)
		}
		sol, err := buildSolution(ns, name, uid, rv, createdAt, labelsJSON, specSolutionName, statusPhase, conditionsJSON)
		if err != nil {
			return nil, err
		}

		// Apply remaining field selectors in memory (e.g. metadata.name, metadata.namespace).
		if fieldSel != nil && !fieldSel.Empty() {
			_, fieldSet, _ := GetAttrs(sol)
			if !fieldSel.Matches(fieldSet) {
				continue
			}
		}
		list.Items = append(list.Items, *sol)
	}
	return list, rows.Err()
}

func (s *PostgresStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	sol := obj.(*internal.Solution)
	ns, _ := request.NamespaceFrom(ctx)
	if sol.Namespace == "" {
		sol.Namespace = ns
	}
	if err := createValidation(ctx, obj); err != nil {
		return nil, err
	}

	sol.UID = types.UID(uuid.NewUUID())
	sol.ResourceVersion = "1"
	sol.CreationTimestamp = metav1.NewTime(time.Now())

	labelsJSON, err := json.Marshal(sol.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	conditionsJSON, err := json.Marshal(sol.Status.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}

	_, err = s.db.Exec(ctx,
		`INSERT INTO solutions
		 (namespace, name, uid, resource_version, creation_timestamp, labels,
		  spec_solution_name, status_phase, status_conditions)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ns, sol.Name, string(sol.UID), 1, sol.CreationTimestamp.Time,
		labelsJSON, sol.Spec.SolutionName, string(sol.Status.Phase), conditionsJSON,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errors.NewAlreadyExists(solutionGR, sol.Name)
		}
		return nil, fmt.Errorf("insert solution: %w", err)
	}
	if payload, err := s.notifyPayloadFor("ADDED", sol); err == nil {
		_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
	}
	return sol.DeepCopyObject(), nil
}

func (s *PostgresStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, _ bool, _ *metav1.UpdateOptions) (runtime.Object, bool, error) {
	ns, _ := request.NamespaceFrom(ctx)

	existing, err := s.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, false, err
	}
	updated, err := objInfo.UpdatedObject(ctx, existing)
	if err != nil {
		return nil, false, err
	}
	if err := updateValidation(ctx, updated, existing); err != nil {
		return nil, false, err
	}

	sol := updated.(*internal.Solution)
	existingSol := existing.(*internal.Solution)
	rv, _ := strconv.Atoi(existingSol.ResourceVersion)
	newRV := rv + 1
	sol.ResourceVersion = strconv.Itoa(newRV)

	labelsJSON, err := json.Marshal(sol.Labels)
	if err != nil {
		return nil, false, fmt.Errorf("marshal labels: %w", err)
	}
	conditionsJSON, err := json.Marshal(sol.Status.Conditions)
	if err != nil {
		return nil, false, fmt.Errorf("marshal conditions: %w", err)
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE solutions
		 SET labels=$1, spec_solution_name=$2, status_phase=$3,
		     status_conditions=$4, resource_version=$5
		 WHERE namespace=$6 AND name=$7`,
		labelsJSON, sol.Spec.SolutionName, string(sol.Status.Phase),
		conditionsJSON, newRV, ns, name,
	)
	if err != nil {
		return nil, false, fmt.Errorf("update solution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, false, errors.NewNotFound(solutionGR, name)
	}
	if payload, err := s.notifyPayloadFor("MODIFIED", sol); err == nil {
		_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
	}
	return sol.DeepCopyObject(), false, nil
}

func (s *PostgresStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, _ *metav1.DeleteOptions) (runtime.Object, bool, error) {
	ns, _ := request.NamespaceFrom(ctx)

	existing, err := s.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, false, err
	}
	if err := deleteValidation(ctx, existing); err != nil {
		return nil, false, err
	}

	tag, err := s.db.Exec(ctx, `DELETE FROM solutions WHERE namespace=$1 AND name=$2`, ns, name)
	if err != nil {
		return nil, false, fmt.Errorf("delete solution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, false, errors.NewNotFound(solutionGR, name)
	}
	existingSol := existing.(*internal.Solution)
	if payload, err := s.notifyPayloadFor("DELETED", existingSol); err == nil {
		_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
	}
	return existing, true, nil
}

func (s *PostgresStorage) Watch(ctx context.Context, opts *metainternalversion.ListOptions) (watch.Interface, error) {
	ns, _ := request.NamespaceFrom(ctx)

	var fieldSel fields.Selector
	if opts != nil && opts.FieldSelector != nil {
		if err := validateFieldSelector(opts.FieldSelector); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		fieldSel = opts.FieldSelector
	}

	wctx, cancel := context.WithCancel(ctx)
	w := newWatcher(ns, fieldSel, cancel, wctx)

	s.watchMu.Lock()
	s.watchers = append(s.watchers, w)
	s.watchMu.Unlock()

	return w, nil
}

func (s *PostgresStorage) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, _ *metav1.DeleteOptions, listOpts *metainternalversion.ListOptions) (runtime.Object, error) {
	listed, err := s.List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	sl := listed.(*internal.SolutionList)
	ns, _ := request.NamespaceFrom(ctx)
	for i := range sl.Items {
		obj := &sl.Items[i]
		if err := deleteValidation(ctx, obj); err != nil {
			return nil, err
		}
		if _, err := s.db.Exec(ctx, `DELETE FROM solutions WHERE namespace=$1 AND name=$2`, ns, obj.Name); err != nil {
			return nil, fmt.Errorf("delete collection item %s: %w", obj.Name, err)
		}
	}
	return sl, nil
}

func (s *PostgresStorage) ConvertToTable(ctx context.Context, obj runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(solutionGR).ConvertToTable(ctx, obj, tableOptions)
}

// UpdateStatus implements StatusUpdater: updates only the status field.
func (s *PostgresStorage) UpdateStatus(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)

	existing, err := s.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	updated, err := objInfo.UpdatedObject(ctx, existing)
	if err != nil {
		return nil, err
	}

	updatedSol := updated.(*internal.Solution)
	existingSol := existing.(*internal.Solution)
	rv, _ := strconv.Atoi(existingSol.ResourceVersion)
	newRV := rv + 1

	conditionsJSON, err := json.Marshal(updatedSol.Status.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE solutions
		 SET status_phase=$1, status_conditions=$2, resource_version=$3
		 WHERE namespace=$4 AND name=$5`,
		string(updatedSol.Status.Phase), conditionsJSON, newRV, ns, name,
	)
	if err != nil {
		return nil, fmt.Errorf("update solution status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, errors.NewNotFound(solutionGR, name)
	}

	// Rebuild the result: existing spec + new status + incremented RV.
	existingSol.Status = updatedSol.Status
	existingSol.ResourceVersion = strconv.Itoa(newRV)
	return existingSol.DeepCopyObject(), nil
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanSolution(ns, name string, row scanner) (*internal.Solution, error) {
	var (
		uid, statusPhase string
		rv               int
		createdAt        time.Time
		labelsJSON       []byte
		conditionsJSON   []byte
		specSolutionName string
	)
	if err := row.Scan(&uid, &rv, &createdAt, &labelsJSON, &specSolutionName, &statusPhase, &conditionsJSON); err != nil {
		return nil, err
	}
	return buildSolution(ns, name, uid, rv, createdAt, labelsJSON, specSolutionName, statusPhase, conditionsJSON)
}

func buildSolution(ns, name, uid string, rv int, createdAt time.Time, labelsJSON []byte, specSolutionName, statusPhase string, conditionsJSON []byte) (*internal.Solution, error) {
	var labelMap map[string]string
	if err := json.Unmarshal(labelsJSON, &labelMap); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}
	var conditions []metav1.Condition
	if err := json.Unmarshal(conditionsJSON, &conditions); err != nil {
		return nil, fmt.Errorf("unmarshal conditions: %w", err)
	}
	sol := &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         ns,
			Name:              name,
			UID:               types.UID(uid),
			ResourceVersion:   strconv.Itoa(rv),
			CreationTimestamp: metav1.NewTime(createdAt),
			Labels:            labelMap,
		},
		Spec: internal.SolutionSpec{
			SolutionName: specSolutionName,
		},
		Status: internal.SolutionStatus{
			Phase:      internal.Phase(statusPhase),
			Conditions: conditions,
		},
	}
	return sol, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return fmt.Sprintf("%s", err) != "" && containsStr(err.Error(), "23505")
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findStr(s, substr))
}

func findStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
