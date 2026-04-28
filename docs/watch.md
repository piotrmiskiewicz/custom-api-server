# Testing Watch with kubectl

## Basic watch

Open a terminal and start watching solutions:

```bash
kubectl get solutions -n default -w
```

In a second terminal, create, update, or delete a solution:

```bash
# Create
kubectl apply -f example/solution.yaml

# Delete
kubectl delete solution my-solution3 -n default
```

You will see events stream in the watch terminal as they happen.

## Watch all namespaces

```bash
kubectl get solutions --all-namespaces -w
```

## Watch with field selector

```bash
kubectl get solutions -w --field-selector spec.solutionName=somename
```

Only events for solutions matching `spec.solutionName=somename` will appear.

## Note on reconnects

The watch implementation streams new events only — it does not replay history.
If the watch connection drops and reconnects, events that occurred during the
gap will be missed. For dev purposes this is fine; just re-run the `kubectl get -w`
command to get a fresh list + watch stream.
