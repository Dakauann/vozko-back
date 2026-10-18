package container

import (
	"vozko/infra/netguard"
	"vozko/usecases/workflow/node_executors"
)

func workflowHTTPEgress() node_executors.HTTPEgress {
	return node_executors.HTTPEgress{
		Client:    netguard.NewHTTPClient(node_executors.MaxHTTPRequestTimeout),
		LegacyTLS: netguard.NewLegacyTLSHTTPClient(node_executors.MaxHTTPRequestTimeout),
	}
}
