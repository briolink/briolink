"""Try hand-made faults in reference.go against two fixture copies (base, new)."""
import json, os, subprocess, sys

S = sys.argv[1]
GO = "/usr/local/go/bin/go"
ADD = "invoice.Lines = append(append([]acme.Line(nil), invoice.Lines...), line)"
FAULTS = {
    "add_line prepends": (ADD, "invoice.Lines = append([]acme.Line{line}, invoice.Lines...)"),
    "add_line replaces the lines": (ADD, "invoice.Lines = []acme.Line{line}"),
    "add_line writes into the caller's slice": (ADD, "if len(invoice.Lines) > 0 {\n\t\tinvoice.Lines[0].Description = \"changed\"\n\t}\n\t" + ADD),
    "issue sets issued_at only": ("invoice.Number = &number\n", ""),
    "issue also changes the customer": ("invoice.IssuedAt = &now\n", "invoice.IssuedAt = &now\n\tinvoice.Customer = number\n"),
    "apply_payment sets paid to the amount": ("invoice.Paid += amount", "invoice.Paid = amount"),
    "apply_payment pays the balance": ("invoice.Paid += amount", "invoice.Paid = total(invoice.Lines)"),
    "void also clears paid": ("invoice.VoidReason = &reason\n", "invoice.VoidReason = &reason\n\tinvoice.Paid = 0\n"),
    "void uses a fixed reason": ("invoice.VoidReason = &reason\n", "r := \"void\"\n\tinvoice.VoidReason = &r\n\t_ = reason\n"),
    "total ignores quantity": ("return total(invoice.Lines) }", "var s int64\n\tfor _, l := range invoice.Lines {\n\t\ts += l.UnitPrice\n\t}\n\treturn s\n}"),
    "new_invoice drops the currency": ("return acme.Invoice{Id: id, Customer: customer, Currency: currency}", "_ = currency\n\treturn acme.Invoice{Id: id, Customer: customer}"),
    "settles_balance ignores paid": ("amount == total(invoice.Lines)-invoice.Paid", "amount == total(invoice.Lines)"),
}

def run(fixture, name, old, new):
    ref = os.path.join(S, fixture, "reference", "reference.go")
    src = open(ref).read()
    assert src.count(old) == 1, (name, old)
    mut = os.path.join(S, "f.go")
    open(mut, "w").write(src.replace(old, new))
    ov = os.path.join(S, "f.json")
    json.dump({"Replace": {ref: mut}}, open(ov, "w"))
    r = subprocess.run([GO, "test", "-count=1", "-overlay=" + ov, "./reference/"],
                       cwd=os.path.join(S, fixture), capture_output=True, text=True)
    if "[build failed]" in r.stdout + r.stderr:
        return "invalid"
    return "caught" if r.returncode else "MISSED"

print(f"{'fault':45} {'today':8} {'proposed':8}")
for name, (old, new) in FAULTS.items():
    print(f"{name:45} {run('base', name, old, new):8} {run('new', name, old, new):8}")
