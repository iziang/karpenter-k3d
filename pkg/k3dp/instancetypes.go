package k3dp

import (
	"fmt"
	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/aws/karpenter-core/pkg/cloudprovider"
	"github.com/aws/karpenter-core/pkg/scheduling"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

type InstanceTypeOptions struct {
	Name             string
	Offerings        cloudprovider.Offerings
	Architecture     string
	OperatingSystems sets.String
	Resources        v1.ResourceList
}

const (
	LabelInstanceSize                       = "size"
	ExoticInstanceLabelKey                  = "special"
	IntegerInstanceLabelKey                 = "integer"
	ResourceGPUVendorA      v1.ResourceName = "fake.com/vendor-a"
	ResourceGPUVendorB      v1.ResourceName = "fake.com/vendor-b"
)

func init() {
	// TODO well known labels
	v1alpha5.WellKnownLabels.Insert(
		LabelInstanceSize,
		ExoticInstanceLabelKey,
		IntegerInstanceLabelKey,
	)
}

func priceFromResources(resources v1.ResourceList) float64 {
	price := 0.0
	for k, v := range resources {
		switch k {
		case v1.ResourceCPU:
			price += 0.1 * v.AsApproximateFloat64()
		case v1.ResourceMemory:
			price += 0.1 * v.AsApproximateFloat64() / (1e9)
		case ResourceGPUVendorA, ResourceGPUVendorB:
			price += 1.0
		}
	}
	return price
}

func GetInstanceTypes() []*cloudprovider.InstanceType {
	result := make(map[string]*cloudprovider.InstanceType)
	for _, cpu := range []int{1, 2, 4, 8, 16, 32, 64} {
		for _, scale := range []int{1, 2, 4, 8, 16} { // memory/cpu ratio
			for _, arch := range []string{v1alpha5.ArchitectureAmd64, v1alpha5.ArchitectureArm64} {
				memory := cpu * scale
				name := fmt.Sprintf("%d-%d-%s", cpu, memory, arch)
				opts := InstanceTypeOptions{
					Name:             name,
					Architecture:     arch,
					OperatingSystems: sets.NewString(string(v1.Linux), string(v1.Windows)),
					Resources: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", cpu)),
						v1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dGi", memory)),
					},
				}
				for _, zone := range []string{"test-zone-1", "test-zone-2", "test-zone-3"} {
					for _, ct := range []string{v1alpha5.CapacityTypeSpot, v1alpha5.CapacityTypeOnDemand} {
						price := priceFromResources(opts.Resources)
						opts.Offerings = []cloudprovider.Offering{
							{
								CapacityType: ct,
								Zone:         zone,
								Price:        price,
								Available:    true,
							},
						}
					}
				}
				result[name] = NewInstanceType(opts)
			}
		}
	}
	var instanceTypes []*cloudprovider.InstanceType
	for _, v := range result {
		instanceTypes = append(instanceTypes, v)
	}
	return instanceTypes
}

func NewInstanceType(options InstanceTypeOptions) *cloudprovider.InstanceType {
	if options.Resources == nil {
		options.Resources = map[v1.ResourceName]resource.Quantity{}
	}
	if r := options.Resources[v1.ResourceCPU]; r.IsZero() {
		options.Resources[v1.ResourceCPU] = resource.MustParse("4")
	}
	if r := options.Resources[v1.ResourceMemory]; r.IsZero() {
		options.Resources[v1.ResourceMemory] = resource.MustParse("4Gi")
	}
	if r := options.Resources[v1.ResourcePods]; r.IsZero() {
		options.Resources[v1.ResourcePods] = resource.MustParse("5")
	}
	if len(options.Offerings) == 0 {
		options.Offerings = []cloudprovider.Offering{
			{CapacityType: "spot", Zone: "test-zone-1", Price: priceFromResources(options.Resources), Available: true},
			{CapacityType: "spot", Zone: "test-zone-2", Price: priceFromResources(options.Resources), Available: true},
			{CapacityType: "on-demand", Zone: "test-zone-1", Price: priceFromResources(options.Resources), Available: true},
			{CapacityType: "on-demand", Zone: "test-zone-2", Price: priceFromResources(options.Resources), Available: true},
			{CapacityType: "on-demand", Zone: "test-zone-3", Price: priceFromResources(options.Resources), Available: true},
		}
	}
	if len(options.Architecture) == 0 {
		options.Architecture = "amd64"
	}
	if options.OperatingSystems.Len() == 0 {
		options.OperatingSystems = sets.NewString(string(v1.Linux), string(v1.Windows), "darwin")
	}
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(v1.LabelInstanceTypeStable, v1.NodeSelectorOpIn, options.Name),
		scheduling.NewRequirement(v1.LabelArchStable, v1.NodeSelectorOpIn, options.Architecture),
		scheduling.NewRequirement(v1.LabelOSStable, v1.NodeSelectorOpIn, options.OperatingSystems.List()...),
		scheduling.NewRequirement(v1.LabelTopologyZone, v1.NodeSelectorOpIn, lo.Map(options.Offerings.Available(), func(o cloudprovider.Offering, _ int) string { return o.Zone })...),
		scheduling.NewRequirement(v1alpha5.LabelCapacityType, v1.NodeSelectorOpIn, lo.Map(options.Offerings.Available(), func(o cloudprovider.Offering, _ int) string { return o.CapacityType })...),
		scheduling.NewRequirement(LabelInstanceSize, v1.NodeSelectorOpDoesNotExist),
		scheduling.NewRequirement(ExoticInstanceLabelKey, v1.NodeSelectorOpDoesNotExist),
		scheduling.NewRequirement(IntegerInstanceLabelKey, v1.NodeSelectorOpIn, fmt.Sprint(options.Resources.Cpu().Value())),
	)
	if options.Resources.Cpu().Cmp(resource.MustParse("4")) > 0 &&
		options.Resources.Memory().Cmp(resource.MustParse("8Gi")) > 0 {
		requirements.Get(LabelInstanceSize).Insert("large")
		requirements.Get(ExoticInstanceLabelKey).Insert("optional")
	} else {
		requirements.Get(LabelInstanceSize).Insert("small")
	}

	return &cloudprovider.InstanceType{
		Name:         options.Name,
		Requirements: requirements,
		Offerings:    options.Offerings,
		Capacity:     options.Resources,
		Overhead: &cloudprovider.InstanceTypeOverhead{
			KubeReserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: resource.MustParse("10Mi"),
			},
		},
	}
}
