package workflowsecret

import "context"

type Repository interface {
	Upsert(ctx context.Context, secret Secret) (Secret, error)
	Delete(ctx context.Context, workspaceID, name string) error
	List(ctx context.Context, workspaceID string) ([]Summary, error)
	FindByNames(ctx context.Context, workspaceID string, names []string) ([]Secret, error)
}

type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type Resolver interface {
	Resolve(ctx context.Context, workspaceID string, names []string) (map[string]string, error)
}

type UseCases interface {
	Resolver
	Put(ctx context.Context, workspaceID, userID, name, value string) (Summary, error)
	Delete(ctx context.Context, workspaceID, name string) error
	List(ctx context.Context, workspaceID string) ([]Summary, error)
}
