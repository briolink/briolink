package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is the part of GitHub's REST API the Host uses, for one repository,
// with the quirks the adapter must handle: issues and pull requests share
// numbers and the issues list serves both, unknown labels are created on use,
// a duplicate is a 422, a pull request needs a commit its base lacks, lists
// come in pages linked by a Link header. Every request must carry token.
type fakeAPI struct {
	t     *testing.T
	srv   *httptest.Server
	token string
	page  int // page size cap, to exercise the Link header

	mu         sync.Mutex
	next       int
	issues     map[int]*fIssue
	comments   map[int][]map[string]any
	commentSeq int
	milestones []map[string]any
	refs       map[string]string // branch → sha
	sha        int
	writes     int
}

type fIssue struct {
	Number    int
	Title     string
	Body      string
	State     string
	Labels    []string
	Milestone map[string]any
	Head      string // set for a pull request
	Base      string
}

func newFakeAPI(t *testing.T, repo, token string) *fakeAPI {
	f := &fakeAPI{t: t, token: token, page: 2, issues: map[int]*fIssue{}, comments: map[int][]map[string]any{}, refs: map[string]string{"main": "sha0"}}
	p := "/repos/" + repo
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p+"/issues", f.listIssues)
	mux.HandleFunc("POST "+p+"/issues", f.createIssue)
	mux.HandleFunc("GET "+p+"/issues/{n}", f.withIssue(func(w http.ResponseWriter, r *http.Request, is *fIssue) { f.reply(w, 200, f.issueJSON(is)) }))
	mux.HandleFunc("PATCH "+p+"/issues/{n}", f.withIssue(f.patchIssue))
	mux.HandleFunc("POST "+p+"/issues/{n}/labels", f.withIssue(f.addLabels))
	mux.HandleFunc("DELETE "+p+"/issues/{n}/labels/{name}", f.withIssue(f.removeLabel))
	mux.HandleFunc("GET "+p+"/issues/{n}/comments", f.listComments)
	mux.HandleFunc("POST "+p+"/issues/{n}/comments", f.withIssue(f.comment))
	mux.HandleFunc("GET "+p+"/milestones", f.listMilestones)
	mux.HandleFunc("POST "+p+"/milestones", f.createMilestone)
	mux.HandleFunc("GET "+p+"/git/ref/heads/{ref...}", f.getRef)
	mux.HandleFunc("POST "+p+"/git/refs", f.createRef)
	mux.HandleFunc("GET "+p+"/branches", f.listBranches)
	mux.HandleFunc("PUT "+p+"/contents/{path...}", f.putContents)
	mux.HandleFunc("POST "+p+"/pulls", f.createPull)
	mux.HandleFunc("GET "+p+"/pulls/{n}", f.withIssue(func(w http.ResponseWriter, r *http.Request, is *fIssue) {
		if is.Head == "" {
			f.reply(w, 404, map[string]any{"message": "Not Found"})
			return
		}
		f.reply(w, 200, f.pullJSON(is))
	}))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+f.token {
			t.Errorf("%s %s: Authorization %q", r.Method, r.URL.Path, got)
			f.reply(w, 401, map[string]any{"message": "Bad credentials"})
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodGet {
			f.writes++
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (f *fakeAPI) decode(r *http.Request, v any) {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		f.t.Errorf("%s %s: body: %v", r.Method, r.URL.Path, err)
	}
}

// paged answers one page of items, linking the next one as GitHub does.
func (f *fakeAPI) paged(w http.ResponseWriter, r *http.Request, items []any) {
	size, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if size <= 0 || size > 100 {
		size = 30
	}
	size = min(size, f.page)
	pg, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pg = max(pg, 1)
	lo, hi := min((pg-1)*size, len(items)), min(pg*size, len(items))
	if hi < len(items) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(pg+1))
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next", <%s>; rel="last"`, f.srv.URL, r.URL.Path, q.Encode(), f.srv.URL))
	}
	page := items[lo:hi]
	if page == nil {
		page = []any{}
	}
	f.reply(w, 200, page)
}

func (f *fakeAPI) withIssue(fn func(http.ResponseWriter, *http.Request, *fIssue)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		is, ok := f.issues[n]
		if !ok {
			f.reply(w, 404, map[string]any{"message": "Not Found"})
			return
		}
		fn(w, r, is)
	}
}

func labelsJSON(ls []string) []any {
	out := []any{}
	for _, l := range ls {
		out = append(out, map[string]any{"name": l, "color": "ededed"})
	}
	return out
}

func (f *fakeAPI) issueJSON(is *fIssue) map[string]any {
	m := map[string]any{"number": is.Number, "title": is.Title, "body": nil, "state": is.State, "labels": labelsJSON(is.Labels), "milestone": nil}
	if is.Body != "" {
		m["body"] = is.Body
	}
	if is.Milestone != nil {
		m["milestone"] = is.Milestone
	}
	if is.Head != "" {
		m["pull_request"] = map[string]any{"url": "x"}
	}
	return m
}

func (f *fakeAPI) pullJSON(is *fIssue) map[string]any {
	m := f.issueJSON(is)
	delete(m, "pull_request")
	m["head"] = map[string]any{"ref": is.Head, "sha": f.refs[is.Head]}
	m["base"] = map[string]any{"ref": is.Base, "sha": f.refs[is.Base]}
	return m
}

func (f *fakeAPI) milestone(number int) map[string]any {
	for _, m := range f.milestones {
		if m["number"] == number {
			return m
		}
	}
	return nil
}

func (f *fakeAPI) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	var want []string
	if l := q.Get("labels"); l != "" {
		want = strings.Split(l, ",")
	}
	ms, _ := strconv.Atoi(q.Get("milestone"))
	var items []any
	// Newest first, GitHub's default; the adapter asks for ascending but
	// must not rely on the order.
	for n := f.next; n >= 1; n-- {
		is, ok := f.issues[n]
		if !ok || (state != "all" && is.State != state) {
			continue
		}
		if ms != 0 && (is.Milestone == nil || is.Milestone["number"] != ms) {
			continue
		}
		if !hasAll(is.Labels, want) {
			continue
		}
		items = append(items, f.issueJSON(is))
	}
	f.paged(w, r, items)
}

func hasAll(have, want []string) bool {
	for _, x := range want {
		if !slices.Contains(have, x) {
			return false
		}
	}
	return true
}

func (f *fakeAPI) createIssue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title     string
		Body      string
		Labels    []string
		Milestone int
	}
	f.decode(r, &in)
	if in.Title == "" {
		f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"code": "missing_field", "field": "title"}}})
		return
	}
	is := &fIssue{Title: in.Title, Body: in.Body, State: "open"}
	if in.Milestone != 0 {
		if is.Milestone = f.milestone(in.Milestone); is.Milestone == nil {
			f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"code": "invalid", "field": "milestone"}}})
			return
		}
	}
	f.next++
	is.Number = f.next
	for _, l := range in.Labels {
		if !slices.Contains(is.Labels, l) {
			is.Labels = append(is.Labels, l)
		}
	}
	f.issues[is.Number] = is
	f.reply(w, 201, f.issueJSON(is))
}

func (f *fakeAPI) patchIssue(w http.ResponseWriter, r *http.Request, is *fIssue) {
	var in map[string]string
	f.decode(r, &in)
	if v, ok := in["title"]; ok {
		is.Title = v
	}
	if v, ok := in["body"]; ok {
		is.Body = v
	}
	if v, ok := in["state"]; ok {
		is.State = v
	}
	f.reply(w, 200, f.issueJSON(is))
}

func (f *fakeAPI) addLabels(w http.ResponseWriter, r *http.Request, is *fIssue) {
	var in struct{ Labels []string }
	f.decode(r, &in)
	for _, l := range in.Labels {
		if !slices.Contains(is.Labels, l) {
			is.Labels = append(is.Labels, l)
		}
	}
	f.reply(w, 200, labelsJSON(is.Labels))
}

func (f *fakeAPI) removeLabel(w http.ResponseWriter, r *http.Request, is *fIssue) {
	name := r.PathValue("name")
	i := slices.Index(is.Labels, name)
	if i < 0 {
		f.reply(w, 404, map[string]any{"message": "Label does not exist"})
		return
	}
	is.Labels = slices.Delete(is.Labels, i, i+1)
	f.reply(w, 200, labelsJSON(is.Labels))
}

func (f *fakeAPI) listComments(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.PathValue("n"))
	var items []any
	for _, c := range f.comments[n] {
		items = append(items, c)
	}
	// A missing number lists as empty, as on GitHub.
	f.paged(w, r, items)
}

func (f *fakeAPI) comment(w http.ResponseWriter, r *http.Request, is *fIssue) {
	var in struct{ Body string }
	f.decode(r, &in)
	if in.Body == "" {
		f.reply(w, 422, map[string]any{"message": "Validation Failed"})
		return
	}
	f.commentSeq++
	c := map[string]any{"id": 4000000000 + f.commentSeq, "body": in.Body}
	f.comments[is.Number] = append(f.comments[is.Number], c)
	f.reply(w, 201, c)
}

func (f *fakeAPI) listMilestones(w http.ResponseWriter, r *http.Request) {
	var items []any
	// GitHub sorts by due date; none here, so reverse creation order.
	for i := len(f.milestones) - 1; i >= 0; i-- {
		items = append(items, f.milestones[i])
	}
	f.paged(w, r, items)
}

func (f *fakeAPI) createMilestone(w http.ResponseWriter, r *http.Request) {
	var in struct{ Title, Description string }
	f.decode(r, &in)
	if in.Title == "" {
		f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"code": "missing_field"}}})
		return
	}
	for _, m := range f.milestones {
		if m["title"] == in.Title {
			f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"resource": "Milestone", "code": "already_exists", "field": "title"}}})
			return
		}
	}
	m := map[string]any{"number": len(f.milestones) + 1, "title": in.Title, "description": nil, "state": "open"}
	if in.Description != "" {
		m["description"] = in.Description
	}
	f.milestones = append(f.milestones, m)
	f.reply(w, 201, m)
}

func (f *fakeAPI) getRef(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("ref")
	sha, ok := f.refs[name]
	if !ok {
		f.reply(w, 404, map[string]any{"message": "Not Found"})
		return
	}
	f.reply(w, 200, map[string]any{"ref": "refs/heads/" + name, "object": map[string]any{"sha": sha, "type": "commit"}})
}

func (f *fakeAPI) createRef(w http.ResponseWriter, r *http.Request) {
	var in struct{ Ref, SHA string }
	f.decode(r, &in)
	name, ok := strings.CutPrefix(in.Ref, "refs/heads/")
	if !ok || name == "" {
		f.reply(w, 422, map[string]any{"message": "Reference name is invalid"})
		return
	}
	if _, ok := f.refs[name]; ok {
		f.reply(w, 422, map[string]any{"message": "Reference already exists"})
		return
	}
	f.refs[name] = in.SHA
	f.reply(w, 201, map[string]any{"ref": in.Ref, "object": map[string]any{"sha": in.SHA}})
}

func (f *fakeAPI) listBranches(w http.ResponseWriter, r *http.Request) {
	var items []any
	for _, b := range slices.Sorted(func(yield func(string) bool) {
		for k := range f.refs {
			if !yield(k) {
				return
			}
		}
	}) {
		items = append(items, map[string]any{"name": b})
	}
	f.paged(w, r, items)
}

// putContents commits a file to a branch: the branch moves to a new sha.
func (f *fakeAPI) putContents(w http.ResponseWriter, r *http.Request) {
	var in struct{ Message, Content, Branch string }
	f.decode(r, &in)
	if _, ok := f.refs[in.Branch]; !ok {
		f.reply(w, 404, map[string]any{"message": "Branch not found"})
		return
	}
	f.sha++
	f.refs[in.Branch] = "sha" + strconv.Itoa(f.sha)
	f.reply(w, 201, map[string]any{"commit": map[string]any{"sha": f.refs[in.Branch]}})
}

func (f *fakeAPI) createPull(w http.ResponseWriter, r *http.Request) {
	var in struct{ Title, Body, Head, Base string }
	f.decode(r, &in)
	head, okH := f.refs[in.Head]
	base, okB := f.refs[in.Base]
	switch {
	case in.Title == "" || !okH || !okB:
		f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"code": "invalid", "field": "head"}}})
		return
	case head == base:
		f.reply(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"code": "custom", "message": "No commits between " + in.Base + " and " + in.Head}}})
		return
	}
	f.next++
	is := &fIssue{Number: f.next, Title: in.Title, Body: in.Body, State: "open", Head: in.Head, Base: in.Base}
	f.issues[is.Number] = is
	f.reply(w, 201, f.pullJSON(is))
}
