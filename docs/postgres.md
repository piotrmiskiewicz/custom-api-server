# Querying Postgres in the Cluster

## Connect with psql

Run a one-off `psql` pod that connects to the in-cluster postgres service:

```bash
kubectl run psql --rm -it --restart=Never \
  --image=postgres:16 \
  --env="PGPASSWORD=apiserver" \
  -- psql -h postgres.default.svc.cluster.local -U apiserver -d apiserver
```

This opens an interactive `psql` session. The pod is deleted automatically when you exit.

## List all solutions

```sql
SELECT namespace, name, spec_solution_name, status_phase, creation_timestamp
FROM solutions
ORDER BY creation_timestamp DESC;
```

## Show full details of a specific solution

```sql
SELECT * FROM solutions WHERE namespace = 'default' AND name = 'my-solution';
```

## Count solutions per namespace

```sql
SELECT namespace, COUNT(*) FROM solutions GROUP BY namespace;
```

## Exit

```
\q
```
