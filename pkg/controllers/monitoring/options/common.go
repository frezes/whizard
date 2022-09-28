package options

import (
	"fmt"

	"github.com/kubesphere/whizard/pkg/api/monitoring/v1alpha1"
	"github.com/kubesphere/whizard/pkg/util"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
)

type CommonOptions struct {
	Image           string                      `json:"image,omitempty" yaml:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy           `json:"imagePullPolicy,omitempty" yaml:"imagePullPolicy,omitempty"`
	Affinity        *corev1.Affinity            `json:"affinity,omitempty" yaml:"affinity,omitempty"`
	NodeSelector    map[string]string           `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	Tolerations     []corev1.Toleration         `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	Resources       corev1.ResourceRequirements `json:"resources,omitempty" yaml:"resources,omitempty"`
	Replicas        *int32                      `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	LogLevel        string                      `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	LogFormat       string                      `json:"logFormat,omitempty" yaml:"logFormat,omitempty"`
	Flags           []string                    `json:"flags,omitempty" yaml:"flags,omitempty"`
}

func NewCommonOptions() CommonOptions {
	var replicas int32 = 1
	return CommonOptions{
		Image:    DefaultWhizardImage,
		Replicas: &replicas,
	}
}
func (o *CommonOptions) Validate() []error {
	var errs []error

	if o.Replicas != nil && *o.Replicas < 0 {
		errs = append(errs, fmt.Errorf("replicas must be >= 0"))
	}

	return errs
}

func (o *CommonOptions) ApplyTo(options *CommonOptions) {
	if o.Image != "" {
		options.Image = o.Image
	}

	if o.ImagePullPolicy != "" {
		options.ImagePullPolicy = o.ImagePullPolicy
	}

	if o.Affinity != nil {
		if options.Affinity == nil {
			options.Affinity = o.Affinity
		} else {
			util.Override(options.Affinity, o.Affinity)
		}
	}

	if o.Tolerations != nil {
		if options.Tolerations != nil {
			options.Tolerations = o.Tolerations
		} else {
			util.Override(options.Tolerations, o.Tolerations)
		}
	}

	if o.NodeSelector != nil {
		options.NodeSelector = o.NodeSelector
	}

	if o.Resources.Limits != nil {
		if options.Resources.Limits == nil {
			options.Resources.Limits = o.Resources.Limits
		} else {
			util.Override(options.Resources.Limits, o.Resources.Limits)
		}
	}

	if o.Resources.Requests != nil {
		if options.Resources.Requests == nil {
			options.Resources.Requests = o.Resources.Requests
		} else {
			util.Override(options.Resources.Requests, o.Resources.Requests)
		}
	}

	if o.Replicas != nil {
		options.Replicas = o.Replicas
	}

	if o.LogLevel != "" {
		options.LogLevel = o.LogLevel
	}

	if o.LogFormat != "" {
		options.LogFormat = o.LogFormat
	}

	if o.Flags != nil {
		options.Flags = o.Flags
	}

}

// Override the Options overrides the spec field when it is empty
func (o *CommonOptions) Override(spec *v1alpha1.CommonSpec) {
	if spec.Image == "" {
		spec.Image = o.Image
	}

	if spec.ImagePullPolicy == "" {
		spec.ImagePullPolicy = o.ImagePullPolicy
	}
	if spec.Replicas == nil || *spec.Replicas < 0 {
		spec.Replicas = o.Replicas
	}

	if spec.Affinity == nil {
		spec.Affinity = o.Affinity
	}

	if spec.Tolerations == nil {
		spec.Tolerations = o.Tolerations
	}

	if spec.NodeSelector == nil {
		spec.NodeSelector = o.NodeSelector
	}

	if spec.Resources.Limits == nil {
		spec.Resources.Limits = o.Resources.Limits
	}

	if spec.Resources.Requests == nil {
		spec.Resources.Requests = o.Resources.Requests
	}

	if spec.Replicas == nil {
		spec.Replicas = o.Replicas
	}

	if spec.LogLevel == "" {
		spec.LogLevel = o.LogLevel
	}

	if spec.LogFormat == "" {
		spec.LogFormat = o.LogFormat
	}

	if spec.Flags == nil {
		spec.Flags = o.Flags
	}
}

func (o *CommonOptions) AddFlags(fs *pflag.FlagSet, c *CommonOptions, prefix string) {
	fs.StringVar(&c.Image, prefix+".image", c.Image, "Image with tag/version.")
	fs.StringArrayVar(&c.Flags, prefix+".flags", c.Flags, "Flags with --flag=value.")
}

type SidecarOptions struct {
	// Image is the envoy image with tag/version
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// Define resources requests and limits for envoy container.
	Resources corev1.ResourceRequirements `json:"resources,omitempty" yaml:"resources,omitempty"`
}

func (o *SidecarOptions) AddFlags(fs *pflag.FlagSet, c *SidecarOptions, prefix string) {
	fs.StringVar(&c.Image, prefix+".image", c.Image, "Image with tag/version.")
}

func (o *SidecarOptions) ApplyTo(options *SidecarOptions) {
	if o.Image != "" {
		options.Image = o.Image
	}
	if o.Resources.Limits != nil {
		if options.Resources.Limits == nil {
			options.Resources.Limits = o.Resources.Limits
		} else {
			util.Override(options.Resources.Limits, o.Resources.Limits)
		}
	}

	if o.Resources.Requests != nil {
		if options.Resources.Requests == nil {
			options.Resources.Requests = o.Resources.Requests
		} else {
			util.Override(options.Resources.Requests, o.Resources.Requests)
		}
	}
}

// Override the Options overrides the spec field when it is empty
func (o *SidecarOptions) Override(spec *v1alpha1.SidecarSpec) {
	if spec.Image == "" {
		spec.Image = o.Image
	}
	if spec.Resources.Limits == nil {
		spec.Resources.Limits = o.Resources.Limits
	}

	if spec.Resources.Requests == nil {
		spec.Resources.Requests = o.Resources.Requests
	}
}