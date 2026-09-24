// Package module composes Todo capabilities behind its public binding.
package module

import (
	"context"
	"fmt"

	lifecyclecontract "github.com/domainry/domainry-lifecycle-sdk/contract"
	todocontract "github.com/domainry/domainry-todo-sdk/contract"
	todomodulehost "github.com/domainry/domainry-todo-sdk/modulehost"
	"github.com/domainry/domainry-todo/internal/domain/todo/service"
	store "github.com/domainry/domainry-todo/internal/infrastructure/persistence/database/todo"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

type Factory struct{}

func NewFactory() todomodulehost.Factory { return Factory{} }

func (Factory) OpenModule(ctx context.Context, ref todocontract.ApplicationRef, host todomodulehost.Host) (todomodulehost.ModuleBinding, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if host == nil || host.Database() == nil || host.Dialect() == nil || host.Migrations() == nil {
		return nil, fmt.Errorf("Todo module host is incomplete")
	}
	var authorize store.SourceAuthorizer
	if source, ok := host.(todomodulehost.SourceAuthorizer); ok {
		authorize = func(ctx context.Context, database store.DB, ref string, authority toolsdk.Authority) error {
			return source.AuthorizeTodoSource(ctx, database, ref, authority)
		}
	}
	repository, err := store.Open(ctx, host.Database(), host.Dialect(), host.Profile(), host.Migrations(), authorize)
	if err != nil {
		return nil, fmt.Errorf("open Todo persistence: %w", err)
	}
	return &Binding{runtimeID: ref.RuntimeID, repository: repository}, nil
}

type Binding struct {
	runtimeID  string
	repository *store.Store
}

func (binding *Binding) Todos() todocontract.TodoService { return binding.repository }
func (binding *Binding) Mutations() todomodulehost.TransactionalMutator {
	return binding.repository
}
func (*Binding) Validate(input todocontract.TodoInput) error { return service.Validate(input) }
func (binding *Binding) SubjectLifecycle() lifecyclecontract.SubjectExecutionHandler {
	return store.NewSubjectLifecycle(binding.repository, binding.runtimeID)
}
func (*Binding) Close(context.Context) error { return nil }
