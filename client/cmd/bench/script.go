package main

import (
	"fmt"
	"math/rand"
	"time"

	"gserver/client/pb"
	"gserver/client/pkg/client"
)

type ScriptRunner struct {
	actions  *BotActions
	client   *client.Client
	state    *BotState
	botID    int
	botType  string
	log      *BotLogger
	mixin    *ChatMixinConfig
	silent   bool
	stopCh   chan struct{}
	lastChat time.Time
}

func NewScriptRunner(actions *BotActions, cl *client.Client, state *BotState, botID int, botType string, log *BotLogger, mixin *ChatMixinConfig, silent bool, stopCh chan struct{}) *ScriptRunner {
	return &ScriptRunner{
		actions: actions,
		client:  cl,
		state:   state,
		botID:   botID,
		botType: botType,
		log:     log,
		mixin:   mixin,
		silent:  silent,
		stopCh:  stopCh,
	}
}

func (r *ScriptRunner) RunScript(script []ScriptStep) error {
	for _, step := range script {
		select {
		case <-r.stopCh:
			return nil
		default:
		}
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

func (r *ScriptRunner) dispatch(do string, args map[string]any) func() error {
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
	case "gm":
		return func() error { return r.actions.GM(args) }
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

func (r *ScriptRunner) buildLoop(args map[string]any) func() error {
	count := loopCount(args)
	subScript := parseSubScript(args)
	return func() error {
		return r.runLoop(count, subScript)
	}
}

// loopCount 从 args 读取重复次数;0 表示无限循环。
func loopCount(args map[string]any) int {
	c, ok := args["count"]
	if !ok {
		return 0
	}
	switch n := c.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

// parseSubScript 从 args["script"] 解析子脚本步骤。
func parseSubScript(args map[string]any) []ScriptStep {
	rawScript, ok := args["script"]
	if !ok {
		return nil
	}
	steps, ok := rawScript.([]any)
	if !ok {
		return nil
	}
	var subScript []ScriptStep
	for _, raw := range steps {
		stepMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for k, v := range stepMap {
			argsMap, _ := v.(map[string]any)
			if argsMap == nil {
				argsMap = map[string]any{}
			}
			subScript = append(subScript, ScriptStep{Do: k, Args: argsMap})
		}
	}
	return subScript
}

// runLoop 执行子脚本 count 次;count 为 0 时循环到停止信号。
func (r *ScriptRunner) runLoop(count int, subScript []ScriptStep) error {
	if count != 0 {
		for i := 0; i < count; i++ {
			if err := r.RunScript(subScript); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		select {
		case <-r.stopCh:
			return nil
		default:
		}
		if err := r.RunScript(subScript); err != nil {
			return err
		}
	}
}

func (r *ScriptRunner) retryable(name string, fn func() error) error {
	var lastErr error
	maxRetries := 1
	if name == "login" {
		maxRetries = 3
	}
	start := time.Now()
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		start = time.Now()
		err := fn()
		lat := time.Since(start)
		if err != nil {
			lastErr = err
			r.log.Printf("action=%s lat=%v ok=false error=%v", name, lat, err)
			continue
		}
		if !r.silent {
			r.log.Printf("action=%s lat=%v ok=true", name, lat)
		}
		return nil
	}
	if !r.silent {
		r.log.Printf("action=%s lat=%v ok=false error=%v", name, time.Since(start), lastErr)
	}
	return fmt.Errorf("%s failed after %d retries: %v", name, maxRetries, lastErr)
}

func (r *ScriptRunner) maybeChat() {
	if r.mixin == nil {
		return
	}
	if time.Since(r.lastChat) < 6*time.Second {
		return
	}
	if rand.Float64() >= r.mixin.Chance {
		return
	}
	select {
	case <-r.stopCh:
		return
	default:
	}
	msgText := r.mixin.Messages[rand.Intn(len(r.mixin.Messages))]
	err := r.client.Send(&pb.ReqChatSendChannel{
		ChannelType: r.mixin.Channel,
		Content:     msgText,
	})
	if err != nil {
		r.log.Printf("action=chat error=%v", err)
	} else {
		r.lastChat = time.Now()
		if !r.silent {
			r.log.Printf("action=chat channel=%d ok=true", r.mixin.Channel)
		}
	}
}
