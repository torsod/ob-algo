package main

import (
	"fmt"
	"math/rand"
	"time"
)

// ---------------------------------------------------------------------------
// Types — follows the C pseudo-code architecture from CLAUDE.md
// Price is in deci-dollars (10¢ ticks) to avoid floating point.
// ---------------------------------------------------------------------------

type Price int64
type Qty int64
type OrderID uint64

type Order struct {
	ID      OrderID
	Price   Price
	Qty     Qty
	OrigQty Qty   // original quantity at submission
	Seq     int64 // insertion sequence for time priority
	Prev    *Order
	Next    *Order
}

type PriceLevel struct {
	Price    Price
	TotalQty Qty
	Head     *Order
	Tail     *Order
}

type AuctionBook struct {
	BidLevels map[Price]*PriceLevel
	BidTree   *RBTree // maintains sorted order on every insert/delete

	OrderMap    map[OrderID]*Order
	IsLocked    bool
	TotalBidQty Qty

	nextSeq int64
}

// ---------------------------------------------------------------------------
// Book operations
// ---------------------------------------------------------------------------

func NewAuctionBook() *AuctionBook {
	return &AuctionBook{
		BidLevels: make(map[Price]*PriceLevel),
		BidTree:   NewRBTree(),
		OrderMap:  make(map[OrderID]*Order),
	}
}

func (b *AuctionBook) AddBid(id OrderID, price Price, qty Qty) {
	if b.IsLocked {
		return
	}

	order := &Order{ID: id, Price: price, Qty: qty, OrigQty: qty, Seq: b.nextSeq}
	b.nextSeq++

	// Find or create price level — O(1) lookup, O(log L) tree insert for new levels
	level, ok := b.BidLevels[price]
	if !ok {
		level = &PriceLevel{Price: price}
		b.BidLevels[price] = level
		b.BidTree.Insert(price, level)
	}

	// Enqueue at tail — O(1)
	if level.Tail == nil {
		level.Head = order
		level.Tail = order
	} else {
		order.Prev = level.Tail
		level.Tail.Next = order
		level.Tail = order
	}
	level.TotalQty += qty
	b.TotalBidQty += qty

	// Register in order map — O(1)
	b.OrderMap[id] = order
}

func (b *AuctionBook) Lock() {
	b.IsLocked = true
}

// BestBid returns the highest price level — O(log L) via tree max.
func (b *AuctionBook) BestBid() *PriceLevel {
	return b.BidTree.Max()
}

// ---------------------------------------------------------------------------
// Clearing price calculation
// Sweep from highest bid downward; the clearing price is the lowest price
// level at which cumulative volume first meets or exceeds the ask quantity.
// ---------------------------------------------------------------------------

func (b *AuctionBook) ClearingPrice(askQty Qty) (Price, Qty) {
	var cumulative Qty
	var clearPrice Price

	b.BidTree.DescendingDo(func(level *PriceLevel) {
		if cumulative >= askQty {
			return
		}
		cumulative += level.TotalQty
		clearPrice = level.Price
	})
	return clearPrice, cumulative
}

// ---------------------------------------------------------------------------
// Auction — sweep from best price downward, fill orders in price-time
// priority until the ask quantity is exhausted.
// ---------------------------------------------------------------------------

type FillResult struct {
	OrderID   OrderID
	FilledQty Qty
	FillPrice Price
}

func (b *AuctionBook) RunAuction(askQty Qty, askLimit Price, minAlloc Qty) ([]FillResult, Qty) {
	remaining := askQty
	var fills []FillResult

	b.BidTree.DescendingDo(func(level *PriceLevel) {
		if remaining <= 0 || level.Price < askLimit {
			return
		}

		order := level.Head
		for order != nil && remaining > 0 {
			fillQty := order.Qty
			if fillQty > remaining {
				fillQty = remaining
			}

			// Enforce minimum allocation — skip if under minimum
			// unless this is the last partial fill that exhausts remaining
			if fillQty < minAlloc && fillQty < remaining {
				order = order.Next
				continue
			}

			fills = append(fills, FillResult{
				OrderID:   order.ID,
				FilledQty: fillQty,
				FillPrice: level.Price,
			})

			order.Qty -= fillQty
			remaining -= fillQty

			if order.Qty == 0 {
				order = order.Next
			}
		}
	})
	return fills, askQty - remaining
}

// ---------------------------------------------------------------------------
// main — build book, compute clearing price, run auction
// ---------------------------------------------------------------------------

func main() {
	const (
		askQty    Qty   = 1_000_000
		numOrders       = 10_000
		minPrice  Price = 200 // $20.00 (10¢ ticks)
		maxPrice  Price = 240 // $24.00
		seed            = 0
	)

	start := time.Now()
	rng := rand.New(rand.NewSource(seed))
	book := NewAuctionBook()

	// Generate 1000 buy orders with random prices in [$20.00, $24.00]
	// and random quantities (100–300 shares).
	for i := 0; i < numOrders; i++ {
		price := minPrice + Price(rng.Int63n(int64(maxPrice-minPrice+1)))
		qty := Qty(100 + rng.Int63n(201)) // 100..300
		book.AddBid(OrderID(i+1), price, qty)
	}

	fmt.Printf("Book built: %d orders across %d price levels\n", len(book.OrderMap), len(book.BidLevels))
	fmt.Printf("Total bid volume: %d shares\n", book.TotalBidQty)
	fmt.Println()

	// Lock book and build sorted level index
	book.Lock()

	// Print order book — one line per price level, descending
	fmt.Printf("%-10s %10s %8s\n", "Price", "Volume", "Orders")
	fmt.Println("---------- ---------- --------")
	book.BidTree.DescendingDo(func(level *PriceLevel) {
		count := 0
		for o := level.Head; o != nil; o = o.Next {
			count++
		}
		fmt.Printf("$%-9.2f %10d %8d\n", float64(level.Price)/10.0, level.TotalQty, count)
	})
	fmt.Println()

	// Compute and display clearing price
	clearPrice, cumVol := book.ClearingPrice(askQty)
	fmt.Printf("Clearing price: $%.2f\n", float64(clearPrice)/10.0)
	fmt.Printf("Accumulated volume at clearing price: %d shares\n", cumVol)
	fmt.Println()

	// Run auction: single sell-side order, minimum allocation = 10
	const minAlloc Qty = 10
	fills, totalFilled := book.RunAuction(askQty, clearPrice, minAlloc)

	// Count allocated vs unallocated orders
	allocatedOrders := make(map[OrderID]Qty)
	for _, f := range fills {
		allocatedOrders[f.OrderID] += f.FilledQty
	}

	allocated := len(allocatedOrders)
	unallocated := numOrders - allocated

	fmt.Printf("Auction complete — filled %d shares (min allocation: %d)\n", totalFilled, minAlloc)
	fmt.Printf("Orders receiving allocation:     %d\n", allocated)
	fmt.Printf("Orders NOT receiving allocation: %d\n", unallocated)

	// Report partial fills — the last order at the clearing level may be partial
	partialCount := 0
	for id, filled := range allocatedOrders {
		if filled < book.OrderMap[id].OrigQty {
			fmt.Printf("Partial fill: order %d received %d of %d shares at $%.2f\n",
				id, filled, book.OrderMap[id].OrigQty, float64(book.OrderMap[id].Price)/10.0)
			partialCount++
		}
	}
	if partialCount == 0 {
		fmt.Println("No partial fills")
	}
	fmt.Println()

	elapsed := time.Since(start)
	fmt.Printf("Execution time: %.2f ms\n", float64(elapsed.Microseconds())/1000.0)
}
