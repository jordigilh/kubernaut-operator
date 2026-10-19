package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
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
