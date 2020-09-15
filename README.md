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
oc apply --namespace dowser manifests/config
oc apply --namespace dowser manifests/operator
```

Create a `MetricsCluster` resource specifying the Prow URLs to aggregate into a
discrete Thanos cluster:

```
apiVersion: dowser.dowser/v1
kind: MetricsCluster
metadata:
  name: blocking-46-1w
spec:
  urls:
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-gcp-4.6/1305830510110445568
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1305723582000664576
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-azure-4.6/1305705113293164544
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-gcp-4.6/1305632600848601088
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1305571313192013824
  - https://prow.ci.openshift.org/view/gs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-azure-4.6/1305571314022486016
```

The operator manages a Prometheus instance per distinct URL, and a Thanos query
instance per `MetricsCluster`. Check the routes to find the Thanos URLs:

```
oc get --namespace dowser routes
```

These route URLs can be wired into Grafana as a Prometheus data source.

There's also a tool which can scrape the Prow job history and convert the results
into a SQLite database for easy querying.

For example, run this to construct a database from the last 3 weeks of various jobs:

```
go run . db create \
--from 504h \
--job release-openshift-ocp-installer-e2e-aws-4.6 \
--job release-openshift-ocp-installer-e2e-gcp-4.6 \
--job release-openshift-ocp-installer-e2e-azure-4.6 \
--job release-openshift-origin-installer-e2e-gcp-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-azure-upgrade-4.6 \
--job release-openshift-origin-installer-e2e-aws-upgrade-4.5-stable-to-4.6-ci \
--output-file prow-1w.db
```

The database tool uses upserts, so subsequent imports can be scoped to a shorter
window of time to refresh an existing database.

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
