package gen

// runtimeSrc is the fixed support code every project gets as gen/http/runtime.go:
// problem details, strict JSON reading, and the generic helpers the
// generated decoders and encoders share. It carries nothing project-specific.
const runtimeSrc = `
// Auth tells which role a request is made as; ok is false when the request
// is not authenticated. The project supplies it.
type Auth func(r *http.Request) (role string, ok bool)

// ErrNotFound is what a Service returns (or wraps) when something a request
// names does not exist: the response is 404.
var ErrNotFound = errors.New("not found")

// ErrConflict is what a Service returns (or wraps) when the request clashes
// with the current state of what it names, such as a name already taken: the
// response is 409.
var ErrConflict = errors.New("conflict")

// ErrUnprocessable is what a Service returns (or wraps) when the request is
// well formed but a rule the spec cannot state refuses it: the response is
// 422. The response does not repeat the error's text.
var ErrUnprocessable = errors.New("unprocessable")

// maxBody is the largest JSON body a route reads.
const maxBody = 1 << 20

type fieldError struct {
	In      string ` + "`json:\"in\"`" + `
	Field   string ` + "`json:\"field,omitempty\"`" + `
	Message string ` + "`json:\"message\"`" + `
}

// problems collects what is wrong with a request, all at once.
type problems struct {
	in     string
	status int
	list   []fieldError
}

func (p *problems) add(at, msg string) { p.list = append(p.list, fieldError{p.in, at, msg}) }
func (p *problems) any() bool          { return len(p.list) > 0 }

func (p *problems) code() int {
	if p.status != 0 {
		return p.status
	}
	return http.StatusBadRequest
}

// Problem is the error body (RFC 9457 problem details).
type Problem struct {
	Type   string       ` + "`json:\"type\"`" + `
	Title  string       ` + "`json:\"title\"`" + `
	Status int          ` + "`json:\"status\"`" + `
	Detail string       ` + "`json:\"detail,omitempty\"`" + `
	Errors []fieldError ` + "`json:\"errors,omitempty\"`" + `
}

func writeProblem(w http.ResponseWriter, status int, detail string, errs []fieldError) {
	b, err := json.Marshal(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail, Errors: errs})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	w.Write(b)
}

func writeInvalid(w http.ResponseWriter, p *problems) {
	writeProblem(w, p.code(), "the request is invalid", p.list)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}

// readJSON decodes one JSON object from the body into dst, rejecting
// unknown fields, a wrong Content-Type, a body over maxBody and anything
// after the value. It reports false when there is nothing to decode into
// further; what is wrong has then been added to p. An empty body counts as
// an empty object when allowEmpty.
func readJSON(w http.ResponseWriter, r *http.Request, dst any, p *problems, allowEmpty bool) bool {
	p.in = "body"
	if r.ContentLength == 0 && allowEmpty {
		return true
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		p.status = http.StatusUnsupportedMediaType
		p.add("", "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		if _, err2 := dec.Token(); err2 != io.EOF {
			p.add("", "the body must hold exactly one JSON value")
			return false
		}
		return true
	}
	var tooBig *http.MaxBytesError
	var wrong *json.UnmarshalTypeError
	switch {
	case errors.Is(err, io.EOF):
		if allowEmpty {
			return true
		}
		p.add("", "a JSON body is required")
	case errors.As(err, &tooBig):
		p.status = http.StatusRequestEntityTooLarge
		p.add("", "the body is too large")
	case errors.As(err, &wrong):
		p.add(wrong.Field, "must be "+wantKind(wrong.Type))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		p.add("", strings.TrimPrefix(err.Error(), "json: "))
	default:
		p.add("", "the body is not valid JSON")
	}
	return false
}

func wantKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "an integer"
	case reflect.Bool:
		return "true or false"
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return "an RFC 3339 time"
		}
		return "an object"
	case reflect.Slice:
		return "a list"
	}
	return "a different type"
}

func parseInt(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func parseBool(s string) (bool, bool) {
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func sortedKeys(q url.Values) []string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// join extends a field path.
func join(at, name string) string {
	if at == "" {
		return name
	}
	return at + "." + name
}

func keep[T any](v T, _ string, _ *problems) T { return v }
func same[T any](v T) T                        { return v }
func utc(t time.Time) time.Time                { return t.UTC() }
func ptr[T any](v T) *T                        { return &v }

func decodeList[W, D any](in []W, at string, p *problems, f func(W, string, *problems) D) []D {
	out := make([]D, len(in))
	for i, x := range in {
		out[i] = f(x, fmt.Sprintf("%s[%d]", at, i), p)
	}
	return out
}

func encodeList[D, W any](in []D, f func(D) W) []W {
	out := make([]W, len(in))
	for i, x := range in {
		out[i] = f(x)
	}
	return out
}
`
