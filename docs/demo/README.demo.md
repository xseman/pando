# shop

A basket you can add coffee to.
Everything is in cents.

## Usage

```go
s := store.New()
s.Add("espresso", 250)
s.Add("cortado", 300)
fmt.Println(s.Total()) // 550
```

## Prices

| Item       | Cents | Notes         |
| ---------- | ----: | ------------- |
| espresso   | 250   | a single shot |
| cortado    | 300   | cut with milk |
| flat white | 350   | a double shot |

## Roadmap

- [x] `store.Add` fills the basket
- [x] `store.Total` counts the cents
- [ ] `store.Discount` in percent
- [ ] a `Count` method

> Prices change; the basket does not.

## Development

Run the tests with `go test ./...`,
the program with `go run .`. Nothing
talks to the network.
