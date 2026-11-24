package document

// Command is one undoable edit.
type Command interface {
	Apply(d *Document)
	Revert(d *Document)
}

// History is a command-pattern undo/redo stack.
type History struct {
	undo, redo []Command
	Limit      int
}

// Do applies c and records it.
func (h *History) Do(d *Document, c Command) {
	c.Apply(d)
	h.Push(d, c)
}

// Push records a command that the caller already applied (e.g. an eraser
// drag that removed strokes live).
func (h *History) Push(d *Document, c Command) {
	h.undo = append(h.undo, c)
	h.redo = h.redo[:0]
	if h.Limit > 0 && len(h.undo) > h.Limit {
		h.undo = h.undo[len(h.undo)-h.Limit:]
	}
	d.touch()
}

func (h *History) Undo(d *Document) bool {
	if len(h.undo) == 0 {
		return false
	}
	c := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	c.Revert(d)
	h.redo = append(h.redo, c)
	d.touch()
	return true
}

func (h *History) Redo(d *Document) bool {
	if len(h.redo) == 0 {
		return false
	}
	c := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	c.Apply(d)
	h.undo = append(h.undo, c)
	d.touch()
	return true
}

func (h *History) CanUndo() bool { return len(h.undo) > 0 }
func (h *History) CanRedo() bool { return len(h.redo) > 0 }

// Batch groups commands into one undo step.
type Batch []Command

func (b Batch) Apply(d *Document) {
	for _, c := range b {
		c.Apply(d)
	}
}

func (b Batch) Revert(d *Document) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i].Revert(d)
	}
}

type AddItem struct {
	Page  *Page
	Layer *Layer
	Item  Item
}

func (c *AddItem) Apply(*Document) {
	c.Layer.Items = append(c.Layer.Items, c.Item)
	c.Page.Touch()
}

func (c *AddItem) Revert(*Document) {
	for i := len(c.Layer.Items) - 1; i >= 0; i-- {
		if c.Layer.Items[i] == c.Item {
			c.Layer.Items = append(c.Layer.Items[:i], c.Layer.Items[i+1:]...)
			break
		}
	}
	c.Page.Touch()
}

type RemoveItem struct {
	Page  *Page
	Layer *Layer
	Item  Item
	index int
}

func (c *RemoveItem) Apply(*Document) {
	for i, it := range c.Layer.Items {
		if it == c.Item {
			c.index = i
			c.Layer.Items = append(c.Layer.Items[:i], c.Layer.Items[i+1:]...)
			break
		}
	}
	c.Page.Touch()
}

func (c *RemoveItem) Revert(*Document) {
	i := min(c.index, len(c.Layer.Items))
	c.Layer.Items = append(c.Layer.Items[:i], append([]Item{c.Item}, c.Layer.Items[i:]...)...)
	c.Page.Touch()
}

type InsertPage struct {
	Index int
	Page  *Page
}

func (c *InsertPage) Apply(d *Document) {
	d.Pages = append(d.Pages[:c.Index], append([]*Page{c.Page}, d.Pages[c.Index:]...)...)
}

func (c *InsertPage) Revert(d *Document) {
	d.Pages = append(d.Pages[:c.Index], d.Pages[c.Index+1:]...)
}

type DeletePage struct {
	Index int
	page  *Page
}

func (c *DeletePage) Apply(d *Document) {
	c.page = d.Pages[c.Index]
	d.Pages = append(d.Pages[:c.Index], d.Pages[c.Index+1:]...)
}

func (c *DeletePage) Revert(d *Document) {
	d.Pages = append(d.Pages[:c.Index], append([]*Page{c.page}, d.Pages[c.Index:]...)...)
}

// PageFormat is the part of a page that the size/background controls change.
type PageFormat struct {
	W, H       float32
	Background Background
}

func (p *Page) Format() PageFormat { return PageFormat{p.Width, p.Height, p.Background} }

type SetFormat struct {
	Pages    []*Page
	Old, New []PageFormat
}

func (c *SetFormat) Apply(*Document)  { c.set(c.New) }
func (c *SetFormat) Revert(*Document) { c.set(c.Old) }

func (c *SetFormat) set(f []PageFormat) {
	for i, p := range c.Pages {
		p.Width, p.Height, p.Background = f[i].W, f[i].H, f[i].Background
		p.Touch()
	}
}

// Splice replaces Old with zero or more New items in place (used by the
// partial eraser, which splits strokes, and by move/scale/recolour).
type Splice struct {
	Page  *Page
	Layer *Layer
	Old   Item
	New   []Item
	index int
}

func (c *Splice) Apply(*Document) {
	c.index = -1
	for i, it := range c.Layer.Items {
		if it == c.Old {
			c.index = i
			break
		}
	}
	if c.index < 0 {
		return
	}
	items := append([]Item(nil), c.Layer.Items[:c.index]...)
	items = append(items, c.New...)
	c.Layer.Items = append(items, c.Layer.Items[c.index+1:]...)
	c.Page.Touch()
}

func (c *Splice) Revert(*Document) {
	if c.index < 0 {
		return
	}
	items := append([]Item(nil), c.Layer.Items[:c.index]...)
	items = append(items, c.Old)
	c.Layer.Items = append(items, c.Layer.Items[c.index+len(c.New):]...)
	c.Page.Touch()
}

type MovePage struct{ From, To int }

func (c *MovePage) Apply(d *Document)  { movePage(d, c.From, c.To) }
func (c *MovePage) Revert(d *Document) { movePage(d, c.To, c.From) }

func movePage(d *Document, from, to int) {
	p := d.Pages[from]
	d.Pages = append(d.Pages[:from], d.Pages[from+1:]...)
	d.Pages = append(d.Pages[:to], append([]*Page{p}, d.Pages[to:]...)...)
}

// touch records an edit.
func (d *Document) touch() {
	d.Dirty = true
	d.Edits++
}
