package stage

import "context"

type ContainerPipelineResolver interface {
	PipelineIDForContainer(ctx context.Context, containerID string) (string, error)
}
