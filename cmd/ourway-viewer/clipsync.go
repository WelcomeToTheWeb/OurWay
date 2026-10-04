package main

import "sync"

// maxClipboardBytes matches the agent's limit.
const maxClipboardBytes = 1 << 20

// Clipboard is the local text clipboard.
type Clipboard interface {
	Seq() uint32
	Get() (string, error)
	Set(text string) error
}

// ClipSync keeps the local and remote clipboards in step without
// echoing a value back to where it came from.
type ClipSync struct {
	Clip Clipboard
	Send func(text string) // pushes local text to the remote

	mu   sync.Mutex
	seq  uint32
	text string
}

// Poll forwards a local clipboard change to the remote. Call it a few
// times a second while sync is enabled.
func (c *ClipSync) Poll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	seq := c.Clip.Seq()
	if seq == c.seq {
		return
	}
	c.seq = seq
	text, err := c.Clip.Get()
	if err != nil || text == "" || len(text) > maxClipboardBytes || text == c.text {
		return
	}
	c.text = text
	c.Send(text)
}

// Remote applies text that arrived from the remote clipboard.
func (c *ClipSync) Remote(text string) {
	if len(text) > maxClipboardBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Clip.Set(text); err != nil {
		return
	}
	c.seq = c.Clip.Seq()
	c.text = text
}

// Prime records the current clipboard as already seen, so enabling sync
// does not immediately push stale content.
func (c *ClipSync) Prime() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq = c.Clip.Seq()
	if t, err := c.Clip.Get(); err == nil {
		c.text = t
	}
}
