# Querying Postgres in the Cluster

## Connect with psql

Exec into the running postgres pod:

```bash
kubectl exec -it deployment/postgres -- psql -U apiserver -d apiserver
```

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
