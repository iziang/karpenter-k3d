package main

import (
	corecontrollers "github.com/aws/karpenter-core/pkg/controllers"
	"github.com/aws/karpenter-core/pkg/controllers/state"
	"github.com/aws/karpenter-core/pkg/operator"
	"github.com/aws/karpenter/pkg/context"
	"github.com/bwagner5/karpenter-k3d/pkg/k3dp"
)

func main() {
	cloudProvider := k3dp.NewCloudProvider(context.Context{})
	ctx, operator := operator.NewOperator()
	operator.WithControllers(ctx, corecontrollers.NewControllers(
		ctx,
		operator.Clock,
		operator.GetClient(),
		operator.KubernetesInterface,
		state.NewCluster(operator.Clock, operator.GetClient(), cloudProvider),
		operator.EventRecorder,
		cloudProvider,
	)...).Start(ctx)
}
