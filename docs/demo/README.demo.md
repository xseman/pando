# shop

A basket you can add coffee to. Everything is in cents.

## Usage

```go
s := store.New()
s.Add("espresso", 250)
s.Add("cortado", 300)
fmt.Println(s.Total(), "cents") // 550 cents
```

## Prices

| Item       | Cents | Size   |
| ---------- | ----: | ------ |
| espresso   | 250   | 30 ml  |
| cortado    | 300   | 90 ml  |
| flat white | 350   | 160 ml |

## Roadmap

- [x] `store.Add` puts something in the basket
- [x] `store.Total` counts the cents
- [ ] `store.Discount` takes a percentage off
- [ ] a `Count` method

> Prices change; the basket does not.

## Development

Run the tests with `go test ./...`, and the program with `go run .`.
Nothing talks to the network, so there is nothing to configure.
