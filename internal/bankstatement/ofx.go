package bankstatement

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// ofxNode is a tolerant representation of an OFX/SGML element. OFX 1.x is
// SGML, where most tags are never closed (`<TRNAMT>-12.34`), while OFX 2.x is
// proper XML — so we cannot use an XML decoder and cannot assume closing tags
// either. One scanner handles both.
type ofxNode struct {
	Name string
	Text string
	// Kids holds pointers so a child taken from the stack stays valid when a
	// later sibling is appended and the slice reallocates.
	Kids []*ofxNode
}

// ParseOFX reads OFX, QFX and QBO files. The three only differ in their
// header block and in which Intuit-specific tags they carry; the transaction
// payload is identical, so one parser covers all of them.
func ParseOFX(data []byte, format, _ string) (*Statement, error) {
	root, err := scanOFX(data)
	if err != nil {
		return nil, err
	}
	st := &Statement{Format: format}
	st.Currency = strings.TrimSpace(root.text("CURDEF"))
	if acct := root.first("BANKACCTFROM", "CCACCTFROM", "ACCTFROM"); acct != nil {
		bank := acct.text("BANKID")
		id := acct.text("ACCTID")
		st.AccountID = strings.TrimSpace(strings.TrimSpace(bank + " " + id))
	}

	trnList := root.first("BANKTRANLIST")
	if trnList == nil {
		trnList = root
	}
	st.Start = ofxDatePtr(trnList.text("DTSTART"))
	st.End = ofxDatePtr(trnList.text("DTEND"))

	for _, trn := range findAll(root, "STMTTRN") {
		line, err := ofxLine(trn)
		if err != nil {
			return nil, err
		}
		st.Lines = append(st.Lines, line)
	}
	// Some QBO exports only wrap transactions in <TRN>; take those too rather
	// than returning an empty statement.
	if len(st.Lines) == 0 {
		for _, trn := range findAll(root, "TRN") {
			if line, err := ofxLine(trn); err == nil {
				st.Lines = append(st.Lines, line)
			}
		}
	}
	if len(st.Lines) == 0 {
		return nil, fmt.Errorf("%w: no transactions found in %s", ErrUnsupportedFormat, format)
	}

	if bal := root.first("LEDGERBAL", "AVAILBAL"); bal != nil {
		if amt, err := parseAmount(bal.text("BALAMT"), ""); err == nil {
			st.Closing = &amt
		}
	}
	applyStatementBounds(st, st.Lines)
	return st, nil
}

func ofxLine(trn *ofxNode) (Line, error) {
	raw := trn.text("DTPOSTED")
	d, err := ofxDate(raw)
	if err != nil {
		return Line{}, fmt.Errorf("%w: %q", errUnparseableDate, raw)
	}
	amtRaw := trn.text("TRNAMT")
	// OFX signs are already in our convention: negative = money out. The
	// separator is left to detection because some banks emit "1.234,56".
	amount, err := parseAmount(amtRaw, "")
	if err != nil {
		return Line{}, fmt.Errorf("%w: %q", errUnparseableAmount, amtRaw)
	}
	l := Line{
		Date:         d,
		Amount:       amount,
		Payee:        strings.TrimSpace(trn.text("NAME")),
		Description:  strings.TrimSpace(trn.text("MEMO")),
		Reference:    strings.TrimSpace(trn.text("REFNUM")),
		ChequeNumber: strings.TrimSpace(trn.text("CHECKNUM")),
		ProviderTxID: strings.TrimSpace(trn.text("FITID")),
	}
	// QBO puts the payee in <PAYEE><NAME> and the free text in <MEMO>.
	if l.Payee == "" {
		if p := trn.first("PAYEE"); p != nil {
			l.Payee = strings.TrimSpace(p.text("NAME"))
		}
	}
	if l.Reference == "" {
		l.Reference = strings.TrimSpace(trn.text("SIC"))
	}
	return l, nil
}

// scanOFX builds the element tree. It is deliberately forgiving: unknown
// entities, unclosed tags and stray whitespace all pass through, because real
// bank exports contain all three.
func scanOFX(data []byte) (*ofxNode, error) {
	root := &ofxNode{Name: "OFX"}
	stack := []*ofxNode{root}
	i := 0
	for i < len(data) {
		open := bytes.IndexByte(data[i:], '<')
		if open < 0 {
			break
		}
		open += i
		close := bytes.IndexByte(data[open:], '>')
		if close < 0 {
			break
		}
		close += open
		tag := strings.TrimSpace(string(data[open+1 : close]))
		next := close + 1
		i = next

		switch {
		case tag == "" || strings.HasPrefix(tag, "?") || strings.HasPrefix(tag, "!"):
			continue // declaration, comment or processing instruction
		case strings.HasPrefix(tag, "/"):
			name := strings.ToUpper(tag[1:])
			for len(stack) > 1 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if strings.EqualFold(top.Name, name) {
					break
				}
			}
			continue
		}

		selfClosing := strings.HasSuffix(tag, "/")
		tag = strings.TrimSuffix(tag, "/")
		// Strip attributes: <TAG attr="x"> — the name is all we need.
		if sp := strings.IndexAny(tag, " \t"); sp >= 0 {
			tag = tag[:sp]
		}
		child := &ofxNode{Name: strings.ToUpper(tag)}
		parent := stack[len(stack)-1]
		parent.Kids = append(parent.Kids, child)
		if selfClosing {
			continue
		}
		stack = append(stack, child)

		// Read the text up to the next tag. For SGML that text *is* the value;
		// for XML the following closing tag is discarded with it.
		textStart := i
		nxt := bytes.IndexByte(data[textStart:], '<')
		var text string
		if nxt < 0 {
			text = string(data[textStart:])
			i = len(data)
		} else {
			text = string(data[textStart : textStart+nxt])
			i = textStart + nxt
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		child.Text = text
		// OFX 1.x is SGML and never closes a leaf: `<TRNAMT>-42.50` ends where
		// the next tag begins, so the element must be closed here or every
		// following sibling nests inside it. OFX 2.x is XML and *does* close
		// it, and popping here would leave the closing tag to pop its parent
		// instead — so only close implicitly when no matching closer follows.
		if !nextTagCloses(data, i, child.Name) {
			stack = stack[:len(stack)-1]
		}
	}
	return root, nil
}

// nextTagCloses reports whether the tag at data[i:] is the closing tag of
// `name`, i.e. whether the element that just ended its text is XML-style.
func nextTagCloses(data []byte, i int, name string) bool {
	if i >= len(data) || data[i] != '<' {
		return false
	}
	end := bytes.IndexByte(data[i:], '>')
	if end < 0 {
		return false
	}
	tag := strings.TrimSpace(string(data[i+1 : i+end]))
	if !strings.HasPrefix(tag, "/") {
		return false
	}
	tag = strings.TrimSpace(tag[1:])
	if sp := strings.IndexAny(tag, " \t"); sp >= 0 {
		tag = tag[:sp]
	}
	return strings.EqualFold(tag, name)
}

// text returns the value of the nearest element called `name`: direct
// children are searched first, then the whole subtree. Direct-first matters
// because a QBO `<STMTTRN>` can carry both its own `<NAME>` and a nested
// `<PAYEE><NAME>`, and the direct one is the payee we want.
func (n *ofxNode) text(name string) string {
	if n == nil {
		return ""
	}
	if strings.EqualFold(n.Name, name) && n.Text != "" {
		return n.Text
	}
	for _, kid := range n.Kids {
		if strings.EqualFold(kid.Name, name) {
			return kid.Text
		}
	}
	for _, kid := range n.Kids {
		if v := kid.text(name); v != "" {
			return v
		}
	}
	return ""
}

// first returns the nearest descendant matching any of the names, trying the
// names in the order given so a caller can express a preference (LEDGERBAL
// over AVAILBAL, say) rather than relying on document order.
func (n *ofxNode) first(names ...string) *ofxNode {
	if n == nil {
		return nil
	}
	for _, name := range names {
		if found := n.findFirst(name); found != nil {
			return found
		}
	}
	return nil
}

// findFirst walks the subtree pre-order, so the shallowest and earliest
// match wins.
func (n *ofxNode) findFirst(name string) *ofxNode {
	for _, kid := range n.Kids {
		if strings.EqualFold(kid.Name, name) {
			return kid
		}
	}
	for _, kid := range n.Kids {
		if found := kid.findFirst(name); found != nil {
			return found
		}
	}
	return nil
}

// findAll collects every descendant matching name, in document order.
func findAll(n *ofxNode, name string) []*ofxNode {
	var out []*ofxNode
	var walk func(*ofxNode)
	walk = func(cur *ofxNode) {
		if cur == nil {
			return
		}
		for _, kid := range cur.Kids {
			if strings.EqualFold(kid.Name, name) {
				out = append(out, kid)
			}
			walk(kid)
		}
	}
	walk(n)
	return out
}

func ofxDatePtr(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t, err := ofxDate(s)
	if err != nil {
		return nil
	}
	return &t
}
