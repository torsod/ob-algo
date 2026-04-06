package main

import (
	"fmt"
	"math/rand"
	"sort"
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
	BidLevels    map[Price]*PriceLevel
	SortedLevels []*PriceLevel // descending price, built at lock
	LevelCount   int
	LevelsDirty  bool

	BestBid     *PriceLevel
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
		OrderMap:  make(map[OrderID]*Order),
	}
}

func (b *AuctionBook) AddBid(id OrderID, price Price, qty Qty) {
	if b.IsLocked {
		return
	}

	order := &Order{ID: id, Price: price, Qty: qty, OrigQty: qty, Seq: b.nextSeq}
	b.nextSeq++

	// Find or create price level — O(1)
	level, ok := b.BidLevels[price]
	if !ok {
		level = &PriceLevel{Price: price}
		b.BidLevels[price] = level
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

	// Update best bid — O(1)
	if b.BestBid == nil || price > b.BestBid.Price {
		b.BestBid = level
	}

	b.LevelsDirty = true
}

func (b *AuctionBook) Lock() {
	b.IsLocked = true

	// Collect all levels into flat array
	b.SortedLevels = make([]*PriceLevel, 0, len(b.BidLevels))
	for _, level := range b.BidLevels {
		b.SortedLevels = append(b.SortedLevels, level)
	}

	// Sort descending by price — O(L log L)
	sort.Slice(b.SortedLevels, func(i, j int) bool {
		return b.SortedLevels[i].Price > b.SortedLevels[j].Price
	})
	b.LevelCount = len(b.SortedLevels)
	b.LevelsDirty = false
}

// ---------------------------------------------------------------------------
// Clearing price calculation
// Sweep from highest bid downward; the clearing price is the lowest price
// level at which cumulative volume first meets or exceeds the ask quantity.
// ---------------------------------------------------------------------------

func (b *AuctionBook) ClearingPrice(askQty Qty) (Price, Qty) {
	var cumulative Qty
	var clearPrice Price

	for _, level := range b.SortedLevels {
		cumulative += level.TotalQty
		clearPrice = level.Price
		if cumulative >= askQty {
			break
		}
	}
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

	for i := 0; i < b.LevelCount && remaining > 0; i++ {
		level := b.SortedLevels[i]
		if level.Price < askLimit {
			break
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
	}
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
	for _, level := range book.SortedLevels {
		count := 0
		for o := level.Head; o != nil; o = o.Next {
			count++
		}
		fmt.Printf("$%-9.2f %10d %8d\n", float64(level.Price)/10.0, level.TotalQty, count)
	}
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
