# cmd/generate

The generators' composition root: it registers the `domain`, `http` and
`sqlite` generators with the generator host ([generate/](../../generate/README.md))
and runs them over this repo's own specs in `spec/` into `gen/`.

```text
go run ./cmd/generate           # rewrite gen/
go run ./cmd/generate -check    # fail if gen/ differs (CI runs this)
```

Never edit `gen/`: change the spec or the generator and run this again (M1).
A plugin's generator is registered here when the plugin is added to
`spec/project.json`.
