# prowdb

A tool to scrape the OpenShift Prow job history and convert the results into a
SQLite database for easy querying.

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
