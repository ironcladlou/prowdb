# Dowser

Tools to help work with Prometheus metrics produced by OpenShift CI jobs.

Automatically manages Thanos query frontends aggregating slices of Prometheus instances hosting metrics
scraped from CI tarballs.

Create a namespace for the operator:
```
oc create namespace dowser
```

Install the operator:
```
oc apply --namespace dowser manifests/operator
```

Create a `MetricsCluster` resource (stuffed in a configmap, for now) that specifies
the Prow URLs (from the database) to aggregate into a discrete Thanos cluster:

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

There's also a tool which can scrape the Prow job history and convert the results
into a SQLite database for easy querying.

For example, run this to construct a database from the last week of various jobs:

```
go run . db create \
--from 168h \
--job release-openshift-ocp-installer-e2e-aws-4.6 \
--job release-openshift-ocp-installer-e2e-gcp-4.6 \
--job release-openshift-ocp-installer-e2e-azure-4.6 \
--job release-openshift-origin-installer-e2e-gcp-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-azure-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-ci \
--output-file prow-1w.db
```

Now you can do things like easily discover the URLs for the last week of a set
of jobs capped at one per day:

```
select url
from jobs
where result = 'SUCCESS'
and name in ('release-openshift-ocp-installer-e2e-aws-4.6', 'release-openshift-ocp-installer-e2e-gcp-4.6', 'release-openshift-ocp-installer-e2e-azure-4.6')
group by date(started), name
order by datetime(started) desc, name asc;
```
