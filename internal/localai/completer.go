package localai

import (
	"context"
	"sync"
	"time"
	"unicode/utf8"
)

type Completion struct {
	Query   string
	Command string
	Model   string
	Err     error
}

type Completer struct {
	client   *Client
	model    string
	debounce time.Duration
	results  chan Completion

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

func NewCompleter(cfg ProviderConfig, debounce time.Duration) *Completer {
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	return &Completer{
		client:   NewClient(cfg),
		model:    cfg.Model,
		debounce: debounce,
		results:  make(chan Completion, 1),
	}
}

func (c *Completer) Results() <-chan Completion {
	return c.results
}

func (c *Completer) Request(query string, env Environment) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errCompleterClosed
	}
	if c.cancel != nil {
		c.cancel()
	}
	if utf8.RuneCountInString(query) < 3 {
		c.cancel = nil
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.mu.Unlock()

	go func() {
		timer := time.NewTimer(c.debounce)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		env = EnrichEnvironment(env)
		if ctx.Err() != nil {
			return
		}
		command, err := c.client.Suggest(ctx, query, env)
		for attempt := 0; err != nil && attempt < 2 && ctx.Err() == nil; attempt++ {
			delay := time.Duration(attempt+1) * 500 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			command, err = c.client.Suggest(ctx, query, env)
		}
		if ctx.Err() != nil {
			return
		}
		result := Completion{Query: query, Command: command, Model: c.model, Err: err}
		select {
		case c.results <- result:
		default:
			select {
			case <-c.results:
			default:
			}
			select {
			case c.results <- result:
			default:
			}
		}
	}()
	return nil
}

func (c *Completer) Cancel() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
}

func (c *Completer) Close() {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
}
