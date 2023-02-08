package main

import (
	"context"
	corecontrollers "github.com/aws/karpenter-core/pkg/controllers"
	"github.com/aws/karpenter-core/pkg/controllers/state"
	"github.com/aws/karpenter-core/pkg/operator"
	"github.com/bwagner5/karpenter-k3d/pkg/k3dp"
)

func main() {
	ctx, operator := operator.NewOperator()
	cloudProvider := k3dp.NewCloudProvider(context.Background(), operator.GetClient())
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
