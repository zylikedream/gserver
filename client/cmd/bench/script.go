package main

import (
	"fmt"
	"math/rand"
	"time"

	"hy_client/pb"
	"hy_client/pkg/client"
)

type ScriptRunner struct {
	actions *BotActions
	client  *client.Client
	state   *BotState
	botID   int
	botType string
	log     *BotLogger
	mixin   *ChatMixinConfig
}

func NewScriptRunner(actions *BotActions, cl *client.Client, state *BotState, botID int, botType string, log *BotLogger, mixin *ChatMixinConfig) *ScriptRunner {
	return &ScriptRunner{
		actions: actions,
		client:  cl,
		state:   state,
		botID:   botID,
		botType: botType,
		log:     log,
		mixin:   mixin,
	}
}

func (r *ScriptRunner) RunScript(script []ScriptStep) error {
	for _, step := range script {
		if err := r.executeStep(step); err != nil {
			return err
		}
		r.maybeChat()
	}
	return nil
}

func (r *ScriptRunner) executeStep(step ScriptStep) error {
	action := r.dispatch(step.Do, step.Args)
	if action == nil {
		r.log.Printf("unknown action: %s", step.Do)
		return nil
	}
	return r.retryable(step.Do, action)
}

func (r *ScriptRunner) dispatch(do string, args map[string]interface{}) func() error {
	switch do {
	case "login":
		return func() error { return r.actions.Login(args) }
	case "wait_range":
		return func() error { return r.actions.WaitRange(args) }
	case "breed":
		return func() error { return r.actions.Breed(args) }
	case "wait_for_breed":
		return func() error { return r.actions.WaitForBreed(args) }
	case "finish_breed":
		return func() error { return r.actions.FinishBreed(args) }
	case "ensure_breed":
		return func() error { return r.actions.EnsureBreed(args) }
	case "claim_task":
		return func() error { return r.actions.ClaimTask(args) }
	case "plant":
		return func() error { return r.actions.Plant(args) }
	case "water":
		return func() error { return r.actions.Water(args) }
	case "wait_for_harvest":
		return func() error { return r.actions.WaitForHarvest(args) }
	case "harvest":
		return func() error { return r.actions.Harvest(args) }
	case "plant_cycle":
		return func() error { return r.actions.PlantCycle(args) }
	case "check_orders":
		return func() error { return r.actions.CheckOrders(args) }
	case "submit_orders":
		return func() error { return r.actions.SubmitOrders(args) }
	case "loop":
		return r.buildLoop(args)
	default:
		return nil
	}
}

func (r *ScriptRunner) buildLoop(args map[string]interface{}) func() error {
	count := 0
	if c, ok := args["count"]; ok {
		switch n := c.(type) {
		case int:
			count = n
		case float64:
			count = int(n)
		}
	}

	var subScript []ScriptStep
	if rawScript, ok := args["script"]; ok {
		if steps, ok := rawScript.([]interface{}); ok {
			for _, raw := range steps {
				if stepMap, ok := raw.(map[string]interface{}); ok {
					for k, v := range stepMap {
						var argsMap map[string]interface{}
						if v != nil {
							argsMap, _ = v.(map[string]interface{})
						}
						if argsMap == nil {
							argsMap = map[string]interface{}{}
						}
						subScript = append(subScript, ScriptStep{Do: k, Args: argsMap})
					}
				}
			}
		}
	}

	return func() error {
		if count == 0 {
			for {
				if err := r.RunScript(subScript); err != nil {
					return err
				}
			}
		} else {
			for i := 0; i < count; i++ {
				if err := r.RunScript(subScript); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func (r *ScriptRunner) retryable(name string, fn func() error) error {
	var lastErr error
	maxRetries := 1
	if name == "login" {
		maxRetries = 3
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		start := time.Now()
		err := fn()
		lat := time.Since(start)
		if err != nil {
			lastErr = err
			r.log.Printf("action=%s lat=%v error=%v", name, lat, err)
			continue
		}
		r.log.Printf("action=%s lat=%v ok=true", name, lat)
		return nil
	}
	return fmt.Errorf("%s failed after %d retries: %v", name, maxRetries, lastErr)
}

func (r *ScriptRunner) maybeChat() {
	if r.mixin == nil {
		return
	}
	if rand.Float64() >= r.mixin.Chance {
		return
	}
	msgText := r.mixin.Messages[rand.Intn(len(r.mixin.Messages))]
	err := r.client.Send(&pb.ReqChatSendChannel{
		ChannelType: r.mixin.Channel,
		Content:     msgText,
	})
	if err != nil {
		r.log.Printf("action=chat error=%v", err)
	} else {
		r.log.Printf("action=chat channel=%d ok=true", r.mixin.Channel)
	}
}
