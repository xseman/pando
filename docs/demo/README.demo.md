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

| Item       | Cents | Size   | Notes                                             |
| ---------- | ----: | ------ | ------------------------------------------------- |
| espresso   | 250   | 30 ml  | a single shot, pulled short and served on its own |
| cortado    | 300   | 90 ml  | espresso cut with as much warm milk               |
| flat white | 350   | 160 ml | a double shot under a thin layer of microfoam     |

## Roadmap

- [x] `store.Add` puts something in the basket
- [x] `store.Total` counts the cents
- [ ] `store.Discount` takes a percentage off
- [ ] a `Count` method

> Prices change; the basket does not.

## Development

Run the tests with `go test ./...`, and the program with `go run .`.
Nothing talks to the network, so there is nothing to configure.
