# mapper-be

Backend SDK for the Mapper platform: schema registry, CSV/XLSX source
adapters, streaming import runtime, plain `net/http` API, and a TUS
resumable-upload extension.

```go
svc := mapper.New(
    mapper.WithFileStore(store),
    mapper.WithSourceAdapter(csv.New()),
    mapper.WithImporter(executor.NewImportExecutor(registry, store, csv.New(), processor)),
)
_ = svc.RegisterSchema(generated.SubscriberSchema) // from mapper-compiler
```

HTTP protocol (`net/http` only):

| Method | Route            | Purpose                              |
| ------ | ---------------- | ------------------------------------ |
| GET    | `/schemas/{id}`  | target shape for the mapping UI      |
| POST   | `/files/analyze` | JSON `{file_id}` or multipart `file` |
| POST   | `/imports/sync`  | run mapping → `ImportProcessor`      |

```bash
go vet ./...
go test ./...
```

Split from the [mapper](https://github.com/peacewalker122/mapper) monorepo.
Schemas are produced by
[mapper-compiler](https://github.com/peacewalker122/mapper-compiler).

## License

Apache-2.0. See [LICENSE](LICENSE).
