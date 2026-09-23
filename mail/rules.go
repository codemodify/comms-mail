package mail

import (
	"fmt"
	"strings"
)

func cloneRules(in []FilterRule) []FilterRule {
	out := make([]FilterRule, len(in))
	for i, r := range in {
		out[i] = r
		if r.Conditions != nil {
			out[i].Conditions = append([]RuleCondition(nil), r.Conditions...)
		}
		if r.Actions != nil {
			out[i].Actions = append([]RuleAction(nil), r.Actions...)
		}
	}
	return out
}

func (r FilterRule) match(m Message) bool {
	if !r.Enabled {
		return false
	}
	if len(r.Conditions) == 0 {
		return false
	}
	for _, c := range r.Conditions {
		if !c.match(m) {
			return false
		}
	}
	return true
}

func (c RuleCondition) match(m Message) bool {
	field := strings.ToLower(strings.TrimSpace(c.Field))
	op := strings.ToLower(strings.TrimSpace(c.Op))
	if op == "" {
		op = "contains"
	}
	val := c.Value
	switch field {
	case "from":
		return matchText(m.From, op, val)
	case "to":
		return matchText(m.To+" "+m.Cc+" "+m.Bcc, op, val)
	case "subject":
		return matchText(m.Subject, op, val)
	case "body":
		return matchText(m.Body+" "+m.HTML, op, val)
	case "attachment":
		return m.HasAttach
	case "unread":
		return !m.Read
	case "tag":
		return hasTag(m.Tags, val)
	default:
		return false
	}
}

func matchText(s, op, val string) bool {
	switch op {
	case "is", "equals", "eq":
		return strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(val))
	default:
		return containsFold(s, val)
	}
}

func applyRuleActions(s ruleHost, msg *Message, actions []RuleAction) (stop bool, err error) {
	for _, a := range actions {
		switch strings.ToLower(a.Type) {
		case "stop":
			return true, nil
		case "tag":
			if a.Tag != "" && !hasTag(msg.Tags, a.Tag) {
				msg.Tags = append(append([]string(nil), msg.Tags...), a.Tag)
			}
		case "markread", "read":
			msg.Read = true
		case "markunread", "unread":
			msg.Read = false
		case "delete":
			if err := s.deleteOne(msg.ID); err != nil {
				return false, err
			}
			return true, nil
		case "move":
			if a.Folder != "" {
				if err := s.moveOne(msg.ID, a.Folder); err != nil {
					return false, err
				}
				msg.Folder = a.Folder
			}
		}
	}
	return false, nil
}

type ruleHost interface {
	deleteOne(id MessageID) error
	moveOne(id MessageID, dest FolderID) error
	indexOf(id MessageID) (int, bool)
	messageAt(i int) *Message
}

func nextRuleID(existing []FilterRule) string {
	n := len(existing) + 1
	return fmt.Sprintf("rule-%02d", n)
}

func demoRules() []FilterRule {
	return []FilterRule{
		{
			ID: "rule-01", Name: "Tag invoices as Work", Enabled: true,
			Conditions: []RuleCondition{{Field: "subject", Op: "contains", Value: "Invoice"}},
			Actions:    []RuleAction{{Type: "tag", Tag: "Work"}},
		},
		{
			ID: "rule-02", Name: "File bulk as Junk", Enabled: true, Stop: true,
			Conditions: []RuleCondition{{Field: "subject", Op: "contains", Value: "[bulk]"}},
			Actions:    []RuleAction{{Type: "move", Folder: FolderAdaJunk}, {Type: "stop"}},
		},
	}
}
