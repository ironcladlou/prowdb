# Dowser

Tools to help work with Prometheus metrics produced by OpenShift CI jobs.

Automatically manages Thanos query frontends aggregating slices of Prometheus instances hosting metrics
scraped from CI tarballs.

Create a namespace for the operator:
```
oc create namespace dowser
```

Create a job database by scraping Prow/GCS (this requires the `gcloud` command):
```
# Collect a week's worth of important periodics jobs.
go run . db create \
--from 168h \
--job release-openshift-ocp-installer-e2e-aws-4.6 \
--job release-openshift-ocp-installer-e2e-gcp-4.6 \
--job release-openshift-ocp-installer-e2e-azure-4.6 \
--job release-openshift-origin-installer-e2e-gcp-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-azure-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-ci \
--output-file prow-1w.json
```

Install the job database into the operator's namespace:
```
oc create configmap --namespace dowser db --from-file=db.json=prow-1w.json
```

Install the operator:
```
oc apply --namespace dowser manifests/operator
```

Create a `MetricsCluster` resource (stuffed in a configmap, for now) that specifies
the Prow URLs (from the database) to aggregate into a discrete Thanos cluster:
```
# Create an aggregation of the three major e2e periodics from the
# last week, capped at one result per day per job. 
go run . db select \
--from=168h --result=SUCCESS --max-per-day=1 \
--db-file prow-1w.json \
--job release-openshift-ocp-installer-e2e-aws-4.6 \
--job release-openshift-ocp-installer-e2e-gcp-4.6 \
--job release-openshift-ocp-installer-e2e-azure-4.6 \
--output cluster=e2e-46-1w | oc apply --namespace dowser -f -
```

Or you could create this manually:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-46-1w
data: |
  cluster.yaml:
    apiVersion: dowser/v1
    kind: MetricsCluster
    spec:
      urls:
      - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1302064249488543744
      - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1301691653089660928
      - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1301322578622681088
```

The operator manages a Prometheus instance per distinct URL, and a Thanos query
instance per ConfigMap. Check the routes to find the Thanos URLs:

```
oc get --namespace dowser routes
```

These route URLs can be wired into Grafana as a Prometheus data source.
