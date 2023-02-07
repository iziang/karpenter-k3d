package k3dp

import (
	"context"
	"fmt"
	"github.com/aws/karpenter-core/pkg/scheduling"
	"math/rand"

	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/aws/karpenter-core/pkg/cloudprovider"
	"github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	"github.com/k3d-io/k3d/v5/pkg/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/logging"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterName = "my-karpenter-cp"
)

type K3DCloudProvider struct {
	cluster    *types.Cluster
	kubeClient kclient.Client
}

func NewCloudProvider(ctx context.Context) cloudprovider.CloudProvider {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("k3d"))
	cluster, err := client.ClusterGet(ctx, runtimes.SelectedRuntime, &types.Cluster{Name: ClusterName})
	if err != nil {
		logging.FromContext(ctx).Errorf("getting k3d cluster %s, %v", ClusterName, err)
	}
	return &K3DCloudProvider{
		cluster: cluster,
	}
}

func (c *K3DCloudProvider) Create(ctx context.Context, machine *v1alpha5.Machine) (*v1alpha5.Machine, error) {
	name := fmt.Sprintf("node-%d", rand.Int())
	if err := client.NodeAddToCluster(ctx, runtimes.SelectedRuntime, &types.Node{
		Name:          name,
		Role:          "agent",
		Image:         "rancher/k3s:v1.23.8-k3s1",
		K3sNodeLabels: machine.GetLabels(),
	}, c.cluster, types.NodeCreateOpts{Wait: true}); err != nil {
		logging.FromContext(ctx).Errorf("creating k3d node %s, %v", name, err)
		return nil, err
	}
	return machine, nil
}

func (c *K3DCloudProvider) IsMachineDrifted(context.Context, *v1alpha5.Machine) (bool, error) {
	return false, nil
}

func (c *K3DCloudProvider) Delete(ctx context.Context, n *v1alpha5.Machine) error {
	if err := client.NodeDelete(ctx, runtimes.SelectedRuntime, &types.Node{Name: n.Name}, types.NodeDeleteOpts{}); err != nil {
		return err
	}
	if err := c.kubeClient.Delete(ctx, n); err != nil {
		return err
	}
	return nil
}

func (c *K3DCloudProvider) Get(context.Context, string, string) (*v1alpha5.Machine, error) {
	return &v1alpha5.Machine{}, nil
}

func (c *K3DCloudProvider) GetInstanceTypes(_ context.Context, provisioner *v1alpha5.Provisioner) ([]*cloudprovider.InstanceType, error) {
	return []*cloudprovider.InstanceType{
		{
			Name: "k3s-large",
			Capacity: v1.ResourceList{
				v1.ResourceCPU:              resource.MustParse("100m"),
				v1.ResourceMemory:           resource.MustParse("128Mi"),
				v1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
				v1.ResourcePods:             resource.MustParse("10"),
			},
			Overhead: &cloudprovider.InstanceTypeOverhead{
				KubeReserved: v1.ResourceList{
					v1.ResourceCPU:              resource.MustParse("10m"),
					v1.ResourceMemory:           resource.MustParse("10Mi"),
					v1.ResourceEphemeralStorage: resource.MustParse("128Mi"),
				},
			},
			Requirements: scheduling.NewRequirements(
				scheduling.NewRequirement(v1.LabelArchStable, v1.NodeSelectorOpIn, "amd64"),
				scheduling.NewRequirement(v1.LabelOSStable, v1.NodeSelectorOpIn, "linux"),
			),
			Offerings: []cloudprovider.Offering{
				{
					CapacityType: "on-demand",
					Zone:         "zone-1",
				},
				{
					CapacityType: "on-demand",
					Zone:         "zone-2",
				},
				{
					CapacityType: "on-demand",
					Zone:         "zone-3",
				},
			},
		},
	}, nil
}

// Name returns the CloudProvider implementation name.
func (c *K3DCloudProvider) Name() string {
	return "k3d"
}
