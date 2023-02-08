package k3dp

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/aws/karpenter-core/pkg/cloudprovider"
	"github.com/aws/karpenter-core/pkg/scheduling"
	"github.com/aws/karpenter-core/pkg/utils/resources"
	"github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	"github.com/k3d-io/k3d/v5/pkg/types"
	k3d "github.com/k3d-io/k3d/v5/pkg/types"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"math/rand"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterName = "my-karpenter-cp"
)

type K3DCloudProvider struct {
	cluster       *types.Cluster
	kubeClient    kclient.Client
	instanceTypes []*cloudprovider.InstanceType
}

func NewCloudProvider(ctx context.Context, kubeClient kclient.Client) cloudprovider.CloudProvider {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("k3d"))
	cluster, err := client.ClusterGet(ctx, runtimes.SelectedRuntime, &types.Cluster{Name: ClusterName})
	if err != nil {
		logging.FromContext(ctx).Errorf("getting k3d cluster %s, %v", ClusterName, err)
	}
	return &K3DCloudProvider{
		cluster:       cluster,
		kubeClient:    kubeClient,
		instanceTypes: GetInstanceTypes(),
	}
}

func (c *K3DCloudProvider) Create(ctx context.Context, machine *v1alpha5.Machine) (*v1alpha5.Machine, error) {
	name := fmt.Sprintf("node-%d", rand.Int())
	its, err := c.resolveInstanceTypes(ctx, machine)
	if err != nil {
		return nil, fmt.Errorf("resolve instance types, %w", err)
	}
	if len(its) == 0 {
		return nil, fmt.Errorf("no available instance types")
	}
	instanceType := its[0]
	labels := make(map[string]string)
	for key, requirement := range instanceType.Requirements {
		if requirement.Operator() == v1.NodeSelectorOpIn {
			labels[key] = requirement.Values()[0]
			logging.FromContext(ctx).Infof("name: %s, assign label %s=%s", instanceType.Name, key, requirement.Values()[0])
		}
	}
	// Find Offering
	reqs := scheduling.NewNodeSelectorRequirements(machine.Spec.Requirements...)
	for _, o := range instanceType.Offerings.Available() {
		if reqs.Compatible(scheduling.NewRequirements(
			scheduling.NewRequirement(v1.LabelTopologyZone, v1.NodeSelectorOpIn, o.Zone),
			scheduling.NewRequirement(v1alpha5.LabelCapacityType, v1.NodeSelectorOpIn, o.CapacityType),
		)) == nil {
			labels[v1.LabelTopologyZone] = o.Zone
			labels[v1alpha5.LabelCapacityType] = o.CapacityType
			break
		}
	}

	node := &types.Node{
		Name:          name,
		Role:          "agent",
		Image:         "rancher/k3s:v1.23.8-k3s1",
		K3sNodeLabels: labels,
	}
	if err := client.NodeAddToCluster(ctx, runtimes.SelectedRuntime, node, c.cluster, types.NodeCreateOpts{Wait: true}); err != nil {
		logging.FromContext(ctx).Errorf("creating k3d node %s, %v", name, err)
		return nil, err
	}
	result, err := c.nodeToMachine(ctx, node, instanceType)
	if err != nil {
		return nil, fmt.Errorf("convert node to machine, %w", err)
	}
	return result, nil
}

func (c *K3DCloudProvider) IsMachineDrifted(context.Context, *v1alpha5.Machine) (bool, error) {
	return false, nil
}

func (c *K3DCloudProvider) Delete(ctx context.Context, n *v1alpha5.Machine) error {
	return client.NodeDelete(ctx, runtimes.SelectedRuntime, &types.Node{Name: n.Name}, types.NodeDeleteOpts{})
}

func (c *K3DCloudProvider) Get(ctx context.Context, machineName string, _ string) (*v1alpha5.Machine, error) {
	logging.FromContext(ctx).Info()
	node, err := client.NodeGet(ctx, runtimes.SelectedRuntime, &types.Node{Name: machineName})
	if err != nil {
		return nil, fmt.Errorf("get instance, %w", err)
	}
	it := node.K3sNodeLabels[v1.LabelInstanceType]
	instanceType, found := lo.Find(c.instanceTypes, func(i *cloudprovider.InstanceType) bool {
		return aws.StringValue(&it) == i.Name
	})
	if !found {
		return nil, fmt.Errorf("find instance type, %w", err)
	}
	machine, err := c.nodeToMachine(ctx, node, instanceType)
	if err != nil {
		return nil, fmt.Errorf("convert node to machine, %w", err)
	}
	return machine, nil
}

func (c *K3DCloudProvider) GetInstanceTypes(_ context.Context, provisioner *v1alpha5.Provisioner) ([]*cloudprovider.InstanceType, error) {
	return c.instanceTypes, nil
}

// Name returns the CloudProvider implementation name.
func (c *K3DCloudProvider) Name() string {
	return "k3d"
}

func (c *K3DCloudProvider) resolveInstanceTypes(ctx context.Context, machine *v1alpha5.Machine) ([]*cloudprovider.InstanceType, error) {
	provisionerName, ok := machine.Labels[v1alpha5.ProvisionerNameLabelKey]
	if !ok {
		return nil, fmt.Errorf("finding provisioner owner")
	}
	provisioner := &v1alpha5.Provisioner{}
	if err := c.kubeClient.Get(ctx, k8sTypes.NamespacedName{Name: provisionerName}, provisioner); err != nil {
		return nil, fmt.Errorf("getting provisioner owner, %w", err)
	}
	instanceTypes, err := c.GetInstanceTypes(ctx, provisioner)
	if err != nil {
		return nil, fmt.Errorf("getting instance types, %w", err)
	}
	reqs := scheduling.NewNodeSelectorRequirements(machine.Spec.Requirements...)
	return lo.Filter(instanceTypes, func(i *cloudprovider.InstanceType, _ int) bool {
		if reqs.Compatible(i.Requirements) != nil {
			logging.FromContext(ctx).Infof("[Filter] not compitable with instance type %s", i.Name)
			return false
		}
		if len(i.Offerings.Requirements(reqs).Available()) <= 0 {
			logging.FromContext(ctx).Infof("[Filter] can not find available offerings with instance type %s", i.Name)
			return false
		}
		if !resources.Fits(resources.Merge(machine.Spec.Resources.Requests, i.Overhead.Total()), i.Capacity) {
			logging.FromContext(ctx).Infof("[Filter] resource not fits with instance type %s", i.Name)
			return false
		}
		return true
	}), nil
}

func (c *K3DCloudProvider) nodeToMachine(ctx context.Context, node *k3d.Node, instanceType *cloudprovider.InstanceType) (*v1alpha5.Machine, error) {
	machine := &v1alpha5.Machine{}
	labels := map[string]string{}
	for key, req := range instanceType.Requirements {
		if req.Operator() == v1.NodeSelectorOpIn {
			labels[key] = req.Values()[0]
		}
	}
	labels[v1.LabelTopologyZone] = node.K3sNodeLabels[v1.LabelTopologyZone]
	labels[v1alpha5.LabelCapacityType] = node.K3sNodeLabels[v1alpha5.LabelCapacityType]
	labels[v1alpha5.MachineNameLabelKey] = machine.Name

	machine.Name = node.Name
	machine.Labels = labels

	machine.Status.Capacity = v1.ResourceList{}
	for k, v := range instanceType.Capacity {
		if !resources.IsZero(v) {
			machine.Status.Capacity[k] = v
		}
	}
	machine.Status.Allocatable = v1.ResourceList{}
	for k, v := range instanceType.Allocatable() {
		if !resources.IsZero(v) {
			machine.Status.Allocatable[k] = v
		}
	}
	return machine, nil

}
