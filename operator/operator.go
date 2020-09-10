package operator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logging "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/ironcladlou/ez-thanos-operator/api"
	"github.com/ironcladlou/ez-thanos-operator/db"
)

func init() {
	logging.SetLogger(zap.New())
}

type BuildDatabase []db.Build

type Operator struct {
	Namespace string

	FetcherImage    string
	PrometheusImage string
	ThanosImage     string

	PrometheusMemory string

	log    logr.Logger
	client client.Client
	db     BuildDatabase
}

func NewStartCommand() *cobra.Command {
	operator := &Operator{}

	var dbFile string

	var command = &cobra.Command{
		Use:   "start",
		Short: "Starts the operator.",
		Run: func(cmd *cobra.Command, args []string) {
			mgr, err := manager.New(clientconfig.GetConfigOrDie(), manager.Options{
				Namespace:          operator.Namespace,
				MetricsBindAddress: "0",
			})
			if err != nil {
				panic(err)
			}
			err = routev1.Install(mgr.GetScheme())
			if err != nil {
				panic(err)
			}
			operator.log = logging.Log.WithName("operator")
			operator.client = mgr.GetClient()

			database, err := db.LoadBuilds(dbFile)
			if err != nil {
				panic(err)
			}
			operator.db = database
			if err := operator.Start(mgr); err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&operator.FetcherImage, "fetcher-image", "", "quay.io/fedora/fedora:31-x86_64", "")
	command.Flags().StringVarP(&operator.PrometheusImage, "prometheus-image", "", "quay.io/prometheus/prometheus:v2.17.2", "")
	command.Flags().StringVarP(&operator.ThanosImage, "thanos-image", "", "quay.io/thanos/thanos:v0.14.0", "")
	command.Flags().StringVarP(&operator.Namespace, "namespace", "", "ez-thanos-operator", "")
	command.Flags().StringVarP(&operator.PrometheusMemory, "prometheus-memory", "", "350Mi", "")
	command.Flags().StringVarP(&dbFile, "db-file", "f", path.Join(os.Getenv("HOME"), ".prow-build-cache.json"), "build database file")

	return command
}

func (o *Operator) Start(mgr manager.Manager) error {
	log := o.log.WithName("entrypoint")

	configMapController, err := controller.New("configmap-controller", mgr, controller.Options{
		Reconciler: reconcile.Func(func(request reconcile.Request) (reconcile.Result, error) {
			return o.reconcileConfigMap(request)
		}),
	})
	if err != nil {
		return fmt.Errorf("unable to set up configmap controller: %w", err)
	}
	if err := configMapController.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return fmt.Errorf("unable to watch configmaps: %w", err)
	}

	deploymentController, err := controller.New("deployment-controller", mgr, controller.Options{
		Reconciler: reconcile.Func(func(request reconcile.Request) (reconcile.Result, error) {
			return o.reconcileDeployment(request)
		}),
	})
	if err != nil {
		return fmt.Errorf("unable to set up deployment controller: %w", err)
	}
	if err := deploymentController.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return fmt.Errorf("unable to watch deployment: %w", err)
	}

	log.Info("starting operator")
	return mgr.Start(signals.SetupSignalHandler())
}

func (o *Operator) reconcileDeployment(request reconcile.Request) (reconcile.Result, error) {
	log := o.log.WithValues("controller", "deployment-controller", "request", request)
	log.Info("reconciling deployment")

	deployment := &appsv1.Deployment{}
	err := o.client.Get(context.TODO(), request.NamespacedName, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "couldn't find deployment")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("couldn't fetch deployment: %w", err)
	}

	if value, hasValue := deployment.Labels["app"]; hasValue && value == "prometheus" {
		return o.reconcilePrometheusDeployment(deployment)
	}

	return reconcile.Result{}, nil
}

func (o *Operator) reconcilePrometheusDeployment(deployment *appsv1.Deployment) (reconcile.Result, error) {
	log := o.log.WithValues("controller", "prometheus-deployment-controller", "deployment", deployment.Name)
	log.Info("reconciling prometheus deployment")

	configMaps := &corev1.ConfigMapList{}
	err := o.client.List(context.TODO(), configMaps, &client.ListOptions{Namespace: o.Namespace})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("couldn't fetch configmaps: %w", err)
	}
	isReferenced := false
	for _, cm := range configMaps.Items {
		if _, hasReference := deployment.Spec.Template.Labels[cm.Name]; hasReference {
			isReferenced = true
			break
		}
	}
	if !isReferenced {
		err := o.client.Delete(context.TODO(), deployment)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Error(err, "couldn't find deployment to delete")
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, fmt.Errorf("couldn't delete deployment: %w", err)
		}
		log.Info("deleted deployment with no references", "deployment", deployment.Name)
	}

	return reconcile.Result{}, nil
}

func (o *Operator) reconcileConfigMap(request reconcile.Request) (reconcile.Result, error) {
	log := o.log.WithValues("controller", "configmap-controller", "request", request)
	log.Info("reconciling configmap")

	cm := &corev1.ConfigMap{}
	err := o.client.Get(context.TODO(), request.NamespacedName, cm)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "couldn't find configmap")
			deploymentList := appsv1.DeploymentList{}
			err := o.client.List(context.TODO(), &deploymentList, &client.ListOptions{Namespace: o.Namespace})
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("couldn't list deployments: %w", err)
			}
			for _, deployment := range deploymentList.Items {
				if _, hasReference := deployment.Spec.Template.Labels[cm.Name]; hasReference {
					delete(deployment.Spec.Template.Labels, cm.Name)
					err := o.client.Update(context.TODO(), &deployment)
					if err != nil {
						log.Error(err, "couldn't update deployment to remove reference", "deployment", deployment.Name)
					} else {
						log.Info("removed reference from deployment", "deployment", deployment.Name)
					}
				}
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("couldn't fetch configmap: %w", err)
	}

	data, hasData := cm.Data["cluster.yaml"]
	if !hasData {
		log.Error(nil, "configmap is missing cluster.yaml key")
		return reconcile.Result{}, nil
	}

	cluster := &api.MetricsCluster{
		ObjectMeta: metav1.ObjectMeta{},
	}
	cluster.Namespace = cm.Namespace
	cluster.Name = cm.Name
	err = yaml.Unmarshal([]byte(data), &cluster)
	if err != nil {
		log.Error(err, "configmap has invalid cluster.yaml contents")
		return reconcile.Result{}, nil
	}

	for _, url := range cluster.Spec.URLs {
		var build db.Build
		found := false
		for _, candidate := range o.db {
			if candidate.URL == url {
				build = candidate
				found = true
				break
			}
		}
		if !found {
			log.Error(nil, "unknown build", "url", url)
			continue
		}
		if len(build.PrometheusTarURL) == 0 {
			log.Error(nil, "no prometheus tar URL defined for build", "url", url)
			continue
		}
		prometheusDeploymentName := o.prometheusDeploymentName(build)
		prometheusDeployment := &appsv1.Deployment{}
		hasPrometheusDeployment := true
		err := o.client.Get(context.TODO(), prometheusDeploymentName, prometheusDeployment)
		if err != nil {
			if errors.IsNotFound(err) {
				hasPrometheusDeployment = false
			} else {
				return reconcile.Result{}, fmt.Errorf("couldn't fetch deployment: %w", err)
			}
		}
		desiredPrometheusDeployment := o.prometheusDeploymentManifest(build)
		if hasPrometheusDeployment {
			prometheusDeployment.Spec = desiredPrometheusDeployment.Spec
			prometheusDeployment.Spec.Template.Labels[cluster.Name] = "true"
			if !equality.Semantic.DeepEqual(prometheusDeployment.Spec, desiredPrometheusDeployment.Spec) ||
				!equality.Semantic.DeepEqual(prometheusDeployment.Labels, desiredPrometheusDeployment.Labels) ||
				!equality.Semantic.DeepEqual(prometheusDeployment.Annotations, desiredPrometheusDeployment.Annotations) {
				err := o.client.Update(context.TODO(), prometheusDeployment)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("couldn't update deployment for url %s: %w", url, err)
				} else {
					log.Info("updated deployment", "name", prometheusDeployment.Name, "url", url)
				}
			}
		} else {
			desiredPrometheusDeployment.Spec.Template.Labels[cluster.Name] = "true"
			err := o.client.Create(context.TODO(), desiredPrometheusDeployment)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("couldn't create deployment for url %s: %w", url, err)
			} else {
				log.Info("updated deployment", "name", prometheusDeployment.Name, "url", url)
			}
		}
	}

	storeService := &corev1.Service{}
	storeServiceName := o.thanosStoreServiceName(cluster)
	hasStoreService := true
	err = o.client.Get(context.TODO(), storeServiceName, storeService)
	if err != nil {
		if errors.IsNotFound(err) {
			hasStoreService = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch service: %w", err)
		}
	}
	if !hasStoreService {
		storeService = o.thanosStoreServiceManifest(cluster)
		err = o.client.Create(context.TODO(), storeService)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create service: %w", err)
		} else {
			log.Info("created service", "name", storeService.Name)
		}
	}

	queryDeployment := &appsv1.Deployment{}
	queryDeploymentName := o.thanosQueryDeploymentName(cluster)
	hasQueryDeployment := true
	err = o.client.Get(context.TODO(), queryDeploymentName, queryDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryDeployment = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch deployment: %w", err)
		}
	}
	if !hasQueryDeployment {
		queryDeployment = o.thanosQueryDeploymentManifest(cluster)
		err = o.client.Create(context.TODO(), queryDeployment)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create deployment: %w", err)
		} else {
			log.Info("created deployment", "name", queryDeployment.Name)
		}
	}

	queryService := &corev1.Service{}
	queryServiceName := o.thanosQueryServiceName(cluster)
	hasQueryService := true
	err = o.client.Get(context.TODO(), queryServiceName, queryService)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryService = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch service: %w", err)
		}
	}
	if !hasQueryService {
		queryService = o.thanosQueryServiceManifest(cluster)
		err = o.client.Create(context.TODO(), queryService)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create service: %w", err)
		} else {
			log.Info("created service", "name", queryService.Name)
		}
	}

	queryRoute := &routev1.Route{}
	queryRouteName := o.thanosQueryRouteName(cluster)
	hasQueryRoute := true
	err = o.client.Get(context.TODO(), queryRouteName, queryRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			hasQueryRoute = false
		} else {
			return reconcile.Result{}, fmt.Errorf("couldn't fetch route: %w", err)
		}
	}
	if !hasQueryRoute {
		queryRoute = o.thanosQueryRouteManifest(cluster)
		err = o.client.Create(context.TODO(), queryRoute)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("couldn't create route: %w", err)
		} else {
			log.Info("created route", "name", queryRoute.Name)
		}
	}

	return reconcile.Result{}, nil
}

func (o *Operator) prometheusDeploymentName(build db.Build) types.NamespacedName {
	hash := sha256.Sum256([]byte(build.URL))
	name := fmt.Sprintf("prometheus-%x", hash[:6])
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o *Operator) prometheusDeploymentManifest(build db.Build) *appsv1.Deployment {
	name := o.prometheusDeploymentName(build)
	sharePIDNamespace := true
	var replicas int32 = 1

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
			Labels: map[string]string{
				"app": "prometheus",
			},
			Annotations: map[string]string{
				"url":      build.URL,
				"started":  build.Started.String(),
				"duration": build.Duration.String(),
				"finished": build.Started.Add(build.Duration).String(),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":        "prometheus",
					"prometheus": name.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":        "prometheus",
						"prometheus": name.Name,
					},
					Annotations: map[string]string{
						"url":      build.URL,
						"started":  build.Started.String(),
						"duration": build.Duration.String(),
						"finished": build.Started.Add(build.Duration).String(),
					},
				},
				Spec: corev1.PodSpec{
					ShareProcessNamespace: &sharePIDNamespace,
					Volumes: []corev1.Volume{
						{
							Name: "prometheus-storage-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:       "setup",
							Image:      o.FetcherImage,
							Command:    []string{"/bin/bash", "-c", deploymentInitScript()},
							WorkingDir: "/prometheus/",
							Env: []corev1.EnvVar{
								{
									Name:  "PROMTAR",
									Value: build.PrometheusTarURL,
								},
								{
									Name:  "DEPLOYMENT_NAME",
									Value: name.Name,
								},
								{
									Name:  "PROW_URL",
									Value: build.URL,
								},
								{
									Name:  "PROW_JOB",
									Value: build.Job,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "prometheus",
							Command: []string{
								"/bin/prometheus",
								"--storage.tsdb.max-block-duration=2h",
								"--storage.tsdb.min-block-duration=2h",
								"--web.enable-lifecycle",
								"--storage.tsdb.path=/prometheus",
								"--config.file=/prometheus/prometheus.yml",
							},
							Image: o.PrometheusImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 9090,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse(o.PrometheusMemory),
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(9090),
										Scheme: "HTTP",
									},
								},
							},
						},
						{
							Name: "thanos-sidecar",
							Command: []string{
								"/bin/thanos",
								"sidecar",
								"--tsdb.path=/prometheus",
								"--prometheus.url=http://localhost:9090",
								"--shipper.upload-compacted",
							},
							Image: o.ThanosImage,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "prometheus-storage-volume",
									MountPath: "/prometheus/",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									//"cpu":    resource.MustParse("100m"),
									//"memory": resource.MustParse("500Mi"),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 9090,
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(9090),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o *Operator) thanosStoreServiceName(cluster *api.MetricsCluster) types.NamespacedName {
	name := fmt.Sprintf("store-%s", cluster.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o *Operator) thanosStoreServiceManifest(cluster *api.MetricsCluster) *corev1.Service {
	name := o.thanosStoreServiceName(cluster)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name:     "grpc",
					Port:     10901,
					Protocol: corev1.ProtocolTCP,
				},
				{
					Name:     "http",
					Port:     10902,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app":        "prometheus",
				cluster.Name: "true",
			},
		},
	}
}

func (o *Operator) thanosQueryDeploymentName(cluster *api.MetricsCluster) types.NamespacedName {
	name := fmt.Sprintf("query-%s", cluster.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o *Operator) thanosQueryDeploymentManifest(cluster *api.MetricsCluster) *appsv1.Deployment {
	name := o.thanosQueryDeploymentName(cluster)
	storeServiceName := o.thanosStoreServiceName(cluster)
	var replicas int32 = 1
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
			Labels: map[string]string{
				"app": "thanos-query",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":     "thanos-query",
					"cluster": cluster.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "thanos-query",
						"cluster": cluster.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "query",
							Image: o.ThanosImage,
							Command: []string{
								"/bin/thanos",
								"query",
								"--http-address=0.0.0.0:19192",
								"--store.sd-dns-interval=10s",
								fmt.Sprintf("--store=dnssrv+_grpc._tcp.%s.%s.svc", storeServiceName.Name, storeServiceName.Namespace),
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 19192,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									//"cpu":    resource.MustParse("100m"),
									//"memory": resource.MustParse("500Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								TimeoutSeconds:   1,
								PeriodSeconds:    10,
								SuccessThreshold: 1,
								FailureThreshold: 3,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/",
										Port:   intstr.FromInt(19192),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o *Operator) thanosQueryServiceName(cluster *api.MetricsCluster) types.NamespacedName {
	name := fmt.Sprintf("query-%s", cluster.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o *Operator) thanosQueryServiceManifest(cluster *api.MetricsCluster) *corev1.Service {
	name := o.thanosQueryServiceName(cluster)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     19192,
					Protocol: corev1.ProtocolTCP,
					Name:     "http",
				},
				{
					Port:     10901,
					Protocol: corev1.ProtocolTCP,
					Name:     "grpc",
				},
			},
			Selector: map[string]string{
				"app":     "thanos-query",
				"cluster": cluster.Name,
			},
		},
	}
}

func (o *Operator) thanosQueryRouteName(cluster *api.MetricsCluster) types.NamespacedName {
	name := fmt.Sprintf("query-%s", cluster.Name)
	return types.NamespacedName{Namespace: o.Namespace, Name: name}
}

func (o *Operator) thanosQueryRouteManifest(cluster *api.MetricsCluster) *routev1.Route {
	name := o.thanosQueryRouteName(cluster)
	queryServiceName := o.thanosQueryServiceName(cluster)
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: queryServiceName.Name,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}
}

func deploymentInitScript() string {
	return `set -uxo pipefail
umask 0000
curl -sL ${PROMTAR} | tar xvz -m
chown -R 65534:65534 /prometheus

cat >/prometheus/prometheus.yml <<EOL
# my global config
global:
  external_labels:
    cluster_name: '${DEPLOYMENT_NAME}'
    cluster_url: '${PROW_URL}'
    cluster_job: '${PROW_JOB}'

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']
EOL
`
}
