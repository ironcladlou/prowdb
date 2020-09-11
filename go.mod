module github.com/ironcladlou/dowser

go 1.14

replace (
	k8s.io/api => k8s.io/api v0.18.6
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.6
	k8s.io/client-go => k8s.io/client-go v0.18.6
)

require (
	github.com/go-logr/logr v0.1.0
	github.com/openshift/api v0.0.0-20200520235321-2bd66cee3218
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v1.0.0
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	k8s.io/api v0.18.7-rc.0
	k8s.io/apimachinery v0.18.7-rc.0
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/klog v1.0.0
	k8s.io/test-infra v0.0.0-20200731220739-2efbce017d1a
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/yaml v1.2.0
)
