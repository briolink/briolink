package spec

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	domain "github.com/briolink/briolink/plugins/domain/spec"
)

func pilot(t *testing.T) (*Routes, map[string]*domain.Context) {
	t.Helper()
	r, err := Load("testdata/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.Load("../../domain/spec/testdata/acme.json")
	if err != nil {
		t.Fatal(err)
	}
	return r, map[string]*domain.Context{"acme": c}
}

func TestPilotIsValid(t *testing.T) {
	r, cs := pilot(t)
	if err := r.CheckAgainst(cs); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	data, _ := os.ReadFile("testdata/routes.json")
	bad := strings.Replace(string(data), `"status": 201,`, `"status": 201, "auth": "none",`, 1)
	if _, err := Decode([]byte(bad)); err == nil || !strings.Contains(err.Error(), `unknown field "routes[].auth"`) {
		t.Fatalf("got %v", err)
	}
}

func intp(n int) *int { return &n }

func TestRules(t *testing.T) {
	rt := func(r *Routes, name string) *Route {
		for i := range r.Routes {
			if r.Routes[i].Name == name {
				return &r.Routes[i]
			}
		}
		panic(name)
	}
	req := func(r *Routes, route, param string) *Param {
		x := rt(r, route)
		for i := range x.Request {
			if x.Request[i].Name == param {
				return &x.Request[i]
			}
		}
		panic(param)
	}
	cases := []struct {
		name   string
		break_ func(r *Routes)
		want   string
	}{
		{"format", func(r *Routes) { r.Format = "v2" }, `format: must be "v0"`},
		{"no roles", func(r *Routes) { r.Roles = nil }, "roles: must declare at least one role"},
		{"role name", func(r *Routes) { r.Roles = append(r.Roles, "Admin") }, `roles: "Admin" must be snake_case`},
		{"role twice", func(r *Routes) { r.Roles = append(r.Roles, "member") }, `roles: "member" is declared twice`},
		{"no routes", func(r *Routes) { r.Routes = nil }, "routes: must declare at least one route"},
		{"route name", func(r *Routes) { rt(r, "add_line").Name = "AddLine" }, `routes[1]: name must be snake_case, got "AddLine"`},
		{"route name twice", func(r *Routes) { rt(r, "add_line").Name = "issue_invoice" }, "routes[issue_invoice]: name is used twice"},
		{"method", func(r *Routes) { rt(r, "add_line").Method = "post" }, `method must be GET, POST, PUT, PATCH or DELETE, got "post"`},
		{"path start", func(r *Routes) { rt(r, "add_line").Path = "invoices" }, `routes[add_line].path: "invoices" must start with / and not end with one`},
		{"path trailing slash", func(r *Routes) { rt(r, "add_line").Path = "/invoices/" }, "must start with / and not end with one"},
		{"path segment", func(r *Routes) { rt(r, "create_invoice").Path = "/Invoices" }, `segment "Invoices" must be lower-case words joined by hyphens`},
		{"path param name", func(r *Routes) { rt(r, "issue_invoice").Path = "/invoices/{Id}/issue" }, "param {Id} must be snake_case"},
		{"path param twice", func(r *Routes) { rt(r, "issue_invoice").Path = "/invoices/{id}/{id}" }, "param {id} appears twice"},
		{"path clash", func(r *Routes) { rt(r, "issue_invoice").Path = "/invoices/{key}/lines" }, "POST /invoices/{key}/lines clashes with route add_line"},
		{"role undeclared", func(r *Routes) { rt(r, "add_line").Role = "admin" }, `role "admin" is not declared in roles`},
		{"context name", func(r *Routes) { rt(r, "add_line").Context = "Acme" }, `context must be snake_case, got "Acme"`},
		{"call shape", func(r *Routes) { rt(r, "add_line").Call = "invoice.add-line" }, `call must be Entity.event or a function name, got "invoice.add-line"`},
		{"status", func(r *Routes) { rt(r, "add_line").Status = 202 }, "status must be 200, 201 or 204, got 202"},
		{"204 with body", func(r *Routes) { rt(r, "add_line").Status = 204 }, "status 204 means no response fields"},
		{"200 without body", func(r *Routes) { rt(r, "add_line").Response = nil }, "status 204 means no response fields, and only 204 may have none"},
		{"response name", func(r *Routes) { rt(r, "invoice_total").Response[0].Name = "Total" }, "response[Total]: name must be snake_case"},
		{"response twice", func(r *Routes) {
			x := rt(r, "invoice_total")
			x.Response = append(x.Response, x.Response[0])
		}, "response[total]: declared twice"},
		{"response type", func(r *Routes) { rt(r, "invoice_total").Response[0].Type = "integer" }, `response[total]: invalid type "integer"`},
		{"request name", func(r *Routes) { req(r, "add_line", "line").Name = "Line" }, "request[Line]: name must be snake_case"},
		{"request twice", func(r *Routes) {
			x := rt(r, "add_line")
			x.Request = append(x.Request, x.Request[1])
		}, "request[line]: declared twice"},
		{"request type", func(r *Routes) { req(r, "add_line", "line").Type = "line" }, `request[line]: invalid type "line"`},
		{"request in", func(r *Routes) { req(r, "add_line", "line").In = "header" }, `in must be path, query or body, got "header"`},
		{"path param missing", func(r *Routes) { req(r, "add_line", "id").In = "query" }, `request: path param {id} has no request field with in "path"`},
		{"path field without placeholder", func(r *Routes) { req(r, "add_line", "line").In = "path" }, "request[line]: is in the path but the path has no {line}"},
		{"path param optional", func(r *Routes) { req(r, "add_line", "id").Type = "?string" }, `a path param must be a required string or int, got "?string"`},
		{"query type", func(r *Routes) {
			req(r, "invoice_total", "id").In = "query"
			rt(r, "invoice_total").Path = "/invoices/total"
			req(r, "invoice_total", "id").Type = "Line"
		}, `a query param must be string, int or bool (or a list of them), got "Line"`},
		{"GET body", func(r *Routes) {
			x := rt(r, "invoice_total")
			x.Request = append(x.Request, Param{Name: "x", In: "body", Type: "int"})
		}, "GET takes no body"},
		{"bounds on bool", func(r *Routes) {
			p := req(r, "record_payment", "amount")
			p.Type = "bool"
		}, "min and max apply only to int, string and list types"},
		{"min above max", func(r *Routes) { req(r, "record_payment", "amount").Max = intp(0) }, "min 1 is above max 0"},
		{"negative length", func(r *Routes) { req(r, "create_invoice", "customer").Min = intp(-1) }, "a length bound cannot be negative"},
		{"pattern on int", func(r *Routes) { req(r, "record_payment", "amount").Pattern = "^[0-9]+$" }, "pattern applies only to string types"},
		{"pattern compiles", func(r *Routes) { req(r, "add_line", "id").Pattern = "(" }, "pattern does not compile"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := pilot(t)
			r = clone(t, r)
			c.break_(r)
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
}

func TestCheckAgainstRules(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(r *Routes, cs map[string]*domain.Context)
		want   string
	}{
		{"unknown context", func(r *Routes, cs map[string]*domain.Context) { r.Routes[0].Context = "billing" }, `routes[create_invoice]: context "billing" has no spec/domain/billing.json`},
		{"unknown function", func(r *Routes, cs map[string]*domain.Context) { r.Routes[0].Call = "make_invoice" }, `call "make_invoice" is not a function in acme`},
		{"calls a guard", func(r *Routes, cs map[string]*domain.Context) { r.Routes[0].Call = "has_lines" }, `call "has_lines" is a guard; a route calls an event or a pure function`},
		{"unknown entity", func(r *Routes, cs map[string]*domain.Context) { r.Routes[1].Call = "Order.add_line" }, `call "Order.add_line": Order is not an entity in acme`},
		{"unknown event", func(r *Routes, cs map[string]*domain.Context) { r.Routes[1].Call = "Invoice.remove_line" }, `call "Invoice.remove_line": Invoice has no event "remove_line"`},
		{"no statechart", func(r *Routes, cs map[string]*domain.Context) { cs["acme"].Entity("Invoice").Statechart = nil }, `call "Invoice.add_line": Invoice has no statechart`},
		{"request type undeclared", func(r *Routes, cs map[string]*domain.Context) { r.Routes[0].Request[1].Type = "Money" }, `routes[create_invoice].request[currency]: type "Money" is not declared in acme`},
		{"response type undeclared", func(r *Routes, cs map[string]*domain.Context) { r.Routes[0].Response[0].Type = "Order" }, `routes[create_invoice].response[invoice]: type "Order" is not declared in acme`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, cs := pilot(t)
			c.break_(r, cs)
			err := r.CheckAgainst(cs)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant error containing %q", err, c.want)
			}
		})
	}
}

func clone(t *testing.T, r *Routes) *Routes {
	t.Helper()
	data, _ := json.Marshal(r)
	var out Routes
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}
