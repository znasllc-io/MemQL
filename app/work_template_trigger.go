package app

import (
	"context"
	"fmt"

	"github.com/znasllc-io/memql/component/automations"
	"github.com/znasllc-io/memql/component/events"
)

type workTemplateTrigger struct {
	app      *App
	template *workTemplate
}
type workTemplateStackKey struct{}

func (t *workTemplateTrigger) executor() *automations.Executor {
	return automations.NewExecutor(automations.ExecutorOptions{Logger: t.app.Logger, Engine: t.app.engine, EventBus: t.app.eventBus, StepRegistry: t.app.stepRegistry, AutomationTrigger: t})
}
func (t *workTemplateTrigger) TriggerAutomation(ctx context.Context, name string) (*automations.AutomationExecution, error) {
	return t.TriggerAutomationWithArgs(ctx, name, nil)
}
func (t *workTemplateTrigger) TriggerAutomationWithArgs(ctx context.Context, name string, args map[string]any) (*automations.AutomationExecution, error) {
	stack, _ := ctx.Value(workTemplateStackKey{}).([]string)
	if len(stack) >= 16 {
		return nil, fmt.Errorf("work template invocation nesting exceeds 16")
	}
	for _, caller := range stack {
		if caller == name {
			return nil, fmt.Errorf("recursive work template invocation: %s", name)
		}
	}
	auto := t.template.members[name]
	if auto == nil {
		var err error
		auto, err = t.app.automationLoader.LoadByName(name)
		if err != nil {
			return nil, err
		}
	}
	if err := automationRunRefusal(auto); err != nil {
		return nil, err
	}
	next := append(append([]string(nil), stack...), name)
	ctx = context.WithValue(ctx, workTemplateStackKey{}, next)
	executor := t.executor()
	defer executor.Close()
	result, err := executor.ExecuteWithClientEvent(ctx, auto, "work.subtemplate", &events.Event{Payload: args})
	if err == nil && result.Status != "completed" {
		err = fmt.Errorf("work subtemplate %s ended %s: %s", name, result.Status, result.Error)
	}
	return result, err
}
