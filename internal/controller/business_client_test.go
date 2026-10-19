package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// businessMappedClient keeps unit tests able to exercise optional API
// capability branches without changing the production reconciler contract.
type businessMappedClient struct {
	client.Client
	mapper meta.RESTMapper
}

const policyDeleteOperation = "delete"

func (c businessMappedClient) RESTMapper() meta.RESTMapper { return c.mapper }

func (c businessMappedClient) Watch(ctx context.Context, list client.ObjectList, options ...client.ListOption) (watch.Interface, error) {
	watchClient, ok := c.Client.(client.WithWatch)
	if !ok {
		return nil, fmt.Errorf("underlying business test client does not support watch")
	}
	return watchClient.Watch(ctx, list, options...)
}
