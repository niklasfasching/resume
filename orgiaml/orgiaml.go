package orgiaml

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/niklasfasching/go-org/org"
)

type Unmarshaler interface{ UnmarshalOrg([]org.Node) error }

type Config struct {
	Stringer func([]org.Node) string
}

func New() *Config {
	return &Config{
		Stringer: HTMLStringer,
	}
}

func (c *Config) Unmarshal(input io.Reader, path string, pointer interface{}) error {
	v := reflect.ValueOf(pointer)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("cannot unmarshal into %s", v.Type())
	}
	document := org.New().Parse(input, path)
	if document.Error != nil {
		return document.Error
	}
	return c.unmarshal(document.Nodes, v)
}

func (c *Config) unmarshal(nodes []org.Node, v reflect.Value) error {
	if u, ok := v.Interface().(Unmarshaler); ok {
		return u.UnmarshalOrg(nodes)
	}
	switch v.Elem().Kind() {
	case reflect.Slice:
		return c.unmarshalList(nodes, v)
	case reflect.Map:
		return c.unmarshalMap(nodes, v)
	case reflect.Struct:
		return c.unmarshalStruct(nodes, v)
	case reflect.String:
		v.Elem().SetString(c.Stringer(nodes))
		return nil
	case reflect.Interface:
		if v.Elem().Type().NumMethod() == 0 {
			return c.unmarshalAny(nodes, v)
		}
		fallthrough
	default:
		return fmt.Errorf("cannot unmarshal into %s (unknown type)", v.Type())
	}
}

func (c *Config) unmarshalAny(nodes []org.Node, v reflect.Value) error {
	nodes = withoutEmptyParagraphs(nodes)
	if len(nodes) == 0 {
		return nil
	}
	switch node := nodes[0].(type) {
	case org.List:
		if node.Kind == "descriptive" {
			return c.unmarshalAnyMap(nodes, v)
		}
		return c.unmarshalAnyList(nodes, v)
	case org.Headline:
		if len(node.Tags) == 1 {
			return c.unmarshalAnyMap(nodes, v)
		}
		return c.unmarshalAnyList(nodes, v)
	default:
		var s string
		err := c.unmarshal(nodes, reflect.ValueOf(&s))
		if err != nil {
			return err
		}
		v.Elem().Set(reflect.ValueOf(s))
		return nil
	}
}

func (c *Config) unmarshalAnyMap(nodes []org.Node, v reflect.Value) error {
	t := reflect.MapOf(reflect.TypeOf(""), v.Elem().Type())
	pm := reflect.New(t)
	pm.Elem().Set(reflect.MakeMap(t))
	err := c.unmarshalMap(nodes, pm)
	if err != nil {
		return err
	}
	v.Elem().Set(pm.Elem())
	return nil
}

func (c *Config) unmarshalAnyList(nodes []org.Node, v reflect.Value) error {
	t := reflect.SliceOf(v.Elem().Type())
	ps := reflect.New(t)
	ps.Elem().Set(reflect.MakeSlice(t, 0, 0))
	err := c.unmarshalList(nodes, ps)
	if err != nil {
		return err
	}
	v.Elem().Set(ps.Elem())
	return nil
}

func (c *Config) unmarshalMap(nodes []org.Node, v reflect.Value) error {
	pairs, err := kvPairs(nodes)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := c.unmarshalMapKV(v, pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) unmarshalMapKV(v reflect.Value, key, value []org.Node) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("key (%s): %s", key, recovered)
		}
	}()
	vk := reflect.New(v.Elem().Type().Key())
	if err := c.unmarshal(key, vk); err != nil {
		return err
	}
	vv := reflect.New(v.Elem().Type().Elem())
	if err := c.unmarshal(value, vv); err != nil {
		return err
	}

	v.Elem().SetMapIndex(vk.Elem(), vv.Elem())
	return err
}

func kvPairs(nodes []org.Node) ([][2][]org.Node, error) {
	pairs := [][2][]org.Node{}
	for _, node := range withoutEmptyParagraphs(nodes) {
		switch node := node.(type) {
		case org.Headline:
			if len(node.Tags) != 1 {
				return nil, fmt.Errorf("cannot unmarshal untagged headline as kv pair: %s", node)
			}
			key, value := []org.Node{org.Text{Content: node.Tags[0], IsRaw: false}}, node.Children
			pairs = append(pairs, [2][]org.Node{key, value})
		case org.List:
			if node.Kind == "descriptive" {
				for _, node := range node.Items {
					item, ok := node.(org.DescriptiveListItem)
					if !ok {
						return nil, fmt.Errorf("cannot unmarshal %#v as kv pair", node)
					}
					pairs = append(pairs, [2][]org.Node{item.Term, item.Details})
				}
			} else {
				return nil, fmt.Errorf("cannot unmarshal %#v into map", node)
			}
		default:
			return nil, fmt.Errorf("cannot unmarshal %#v into map", node)
		}
	}
	return pairs, nil
}

func (c *Config) unmarshalList(nodes []org.Node, v reflect.Value) error {
	for _, node := range withoutEmptyParagraphs(nodes) {
		switch node := node.(type) {
		case org.Headline:
			if len(node.Tags) != 0 {
				return fmt.Errorf("cannot unmarshal tagged headline %#v into list", node)
			}
			vv := reflect.New(v.Elem().Type().Elem())
			if err := c.unmarshal(node.Children, vv); err != nil {
				return err
			}
			v.Elem().Set(reflect.Append(v.Elem(), vv.Elem()))
		case org.List:
			if node.Kind == "descriptive" {
				return fmt.Errorf("cannot unmarshal descriptive list %#v into list", node)
			}
			for _, node := range node.Items {
				vv := reflect.New(v.Elem().Type().Elem())
				if err := c.unmarshal(node.(org.ListItem).Children, vv); err != nil {
					return err
				}
				v.Elem().Set(reflect.Append(v.Elem(), vv.Elem()))
			}
		default:
			return fmt.Errorf("cannot unmarshal %#v into list", node)
		}
	}
	return nil
}

func withoutEmptyParagraphs(nodes []org.Node) []org.Node {
	filtered := nodes[:0]
	for _, node := range nodes {
		if strings.TrimSpace(node.String()) != "" {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func (c *Config) unmarshalStruct(nodes []org.Node, v reflect.Value) error {
	pairs, err := kvPairs(nodes)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		i := fieldIndex(v, pair[0])
		if i == -1 {
			continue
		}
		if err := c.unmarshal(pair[1], v.Elem().Field(i).Addr()); err != nil {
			return err
		}
	}
	return nil
}

func fieldIndex(v reflect.Value, keyNodes []org.Node) int {
	t, key := v.Elem().Type(), strings.TrimSpace(org.String(keyNodes))
	for i := 0; i < t.NumField(); i++ {
		name := strings.ToLower(t.Field(i).Name)
		if name == strings.ToLower(key) {
			return i
		}
	}
	return -1
}

func HTMLStringer(nodes []org.Node) string {
	w := org.NewHTMLWriter()
	org.WriteNodes(w, nodes...)
	s := w.String()
	first, last := strings.Index(s, "<p>"), strings.LastIndex(s, "<p>")
	if isSingleParagraph := first == last; isSingleParagraph {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "<p>"), "</p>\n")
	}
	return strings.TrimSpace(s)
}

func OrgStringer(nodes []org.Node) string {
	return strings.TrimSpace(org.String(nodes))
}
