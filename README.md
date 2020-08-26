# OpenShift CI Thanos Operator

Automatically manages Thanos query services aggregating slices of Prometheus instances hosting metrics
scraped from CI tarballs.

Here's how to run it locally, assuming `KUBECONFIG` is set to a cluster to which you have admin permissions.

```
oc create namespace thanos-operator
go run . --namespace thanos-operator
```

Or you can deploy it to an existing Kubernetes or OpenShift cluster.

```
oc create namespace thanos-operator
oc apply --namespace thanos-operator -f manifests/
```

The operator reconciles configurations stored in ConfigMaps.

Create some files like `thanos-a.yaml`:

```yaml
urls:
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1295601070994624512
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1295765814342848512
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1296637077563117568
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1297837010672685056
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1297853566941138944
```

and `thanos-b.yaml`:

```yaml
urls:
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1297837010672685056
- https://prow.ci.openshift.org/view/gcs/origin-ci-test/logs/release-openshift-ocp-installer-e2e-aws-4.6/1297853566941138944
```

Create the ConfigMaps:

```
oc create configmap --namespace thanos-operator thanos-a --from-file=thanos.yaml=thanos-a.yaml
oc create configmap --namespace thanos-operator thanos-b --from-file=thanos.yaml=thanos-b.yaml
```

The operator should set up a Prometheus instance per URL, and a Thanos query instance per ConfigMap. Check the
routes to find the Thanos URLs:

```
oc get --namespace thanos-operator routes
```
