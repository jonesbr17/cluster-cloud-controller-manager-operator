package render

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-cloud-controller-manager-operator/pkg/cloud"
	"github.com/openshift/cluster-cloud-controller-manager-operator/pkg/config"
	"github.com/openshift/cluster-cloud-controller-manager-operator/pkg/substitution"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	bootstrapNamespace = "kube-system"
	bootstrapPrefix    = "bootstrap"
	// bootstrapFileName is built from bootstrapPrefix, resource name and kind
	bootstrapFileName = "%s/%s-%s.yaml"
)

// Render defines render config for use in bootstrap mode
type Render struct {
	// path to rendered configv1.Infrastructure manifest
	infrastructureFile string
	// path to rendered cloud-controller-manager-images ConfigMap manifest for image references to use
	imagesFile string
	// path to populated cloud-config ConfigMap manifest from cluster-config-operator
	// where cloud-config could be extracted for use in CCM static pod
	cloudConfigFile string
}

// New returns controller for render
func New(infrastructureFile, imagesFile, cloudConfigFile string) *Render {
	return &Render{
		infrastructureFile: infrastructureFile,
		imagesFile:         imagesFile,
		cloudConfigFile:    cloudConfigFile,
	}
}

// Run runs boostrap for Machine Config Controller
// It writes all the assets to destDir
func (r *Render) Run(destinationDir string) error {
	infra, imagesMap, err := r.readAssets()
	if err != nil {
		klog.Errorf("Cannot read assets from provided paths: %v", err)
		return err
	}
	config, err := config.ComposeBootstrapConfig(infra, imagesMap, bootstrapNamespace)
	if err != nil {
		klog.Errorf("Cannot compose config for bootstrap render: %v", err)
		return err
	}

	templates := cloud.GetBootstrapResources(config.Platform)
	resources := substitution.FillConfigValues(config, templates)

	for _, resource := range resources {
		klog.Infof("Collected resource %s %q successfully", resource.GetObjectKind().GroupVersionKind(), client.ObjectKeyFromObject(resource))
	}

	return writeAssets(destinationDir, resources)
}

// readAssets collects infrastructure resource and images config map from provided paths
func (r *Render) readAssets() (*configv1.Infrastructure, *corev1.ConfigMap, error) {
	infraData, err := ioutil.ReadFile(r.infrastructureFile)
	if err != nil {
		klog.Errorf("Unable to read data from %q: %v", r.infrastructureFile, err)
		return nil, nil, err
	}

	infra := &configv1.Infrastructure{}
	if err := yaml.UnmarshalStrict(infraData, infra); err != nil {
		klog.Errorf("Cannot decode data into configv1.Infrastructure from %q: %v", r.infrastructureFile, err)
		return nil, nil, err
	}

	imagesData, err := ioutil.ReadFile(r.imagesFile)
	if err != nil {
		klog.Errorf("Unable to read data from %q: %v", r.imagesFile, err)
		return nil, nil, err
	}

	imagesConfigMap := &corev1.ConfigMap{}
	if err := yaml.UnmarshalStrict(imagesData, imagesConfigMap); err != nil {
		klog.Errorf("Cannot decode data into v1.ConfigMap from %q: %v", r.imagesFile, err)
		return nil, nil, err
	}

	return infra, imagesConfigMap, nil
}

// writeAssets writes static pods to disk into <destinationDir>/<bootstrapPrefix>/<resourceName>-<resourceKind>.yaml
func writeAssets(destinationDir string, resources []client.Object) error {
	// Create bootstrap directory in advance to ensure it is present for any provider
	manifestsDir := filepath.Join(destinationDir, bootstrapPrefix)
	if err := os.MkdirAll(manifestsDir, fs.ModePerm); err != nil {
		klog.Errorf("Unable to create destination dir %q: %v", manifestsDir, err)
		return err
	}

	for _, resource := range resources {
		filename := fmt.Sprintf(bootstrapFileName, bootstrapPrefix, resource.GetName(), strings.ToLower(resource.GetObjectKind().GroupVersionKind().Kind))
		path := filepath.Join(destinationDir, filename)

		klog.Infof("Writing file %q on disk", path)
		data, err := yaml.Marshal(resource)
		// As we operate with client.Object we never expect this call to fail.
		utilruntime.HandleError(err)

		if err := os.WriteFile(path, data, 0666); err != nil {
			klog.Errorf("Failed to open %q: %v", path, err)
			return err
		}
	}
	return nil
}
