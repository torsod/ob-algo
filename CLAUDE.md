# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

ob-algo — project is in early/initial setup. No source code, build system, or dependencies configured yet.

Update this file as the project structure, language, build commands, and architecture are established.

## From previous conversations

Good constraint — a single-sided auction book simplifies things significantly. Here's how the architecture changes:

---

### Key Simplifications

Since there is **no continuous matching** and only one side exists until a single auction ask arrives, you can shed several components:

- **No ask-side structures at all** — no ask tree, no ask level map
- **No `try_match` loop** — replace with a single **auction sweep**
- **No best-ask cached pointer**
- **The RBTree can be replaced** — since you only need to sweep bids from highest to lowest at auction time, a **sorted array of price levels** (built once, swept once) outperforms a tree whose O(log n) insertion buys you nothing you actually need at runtime

The dominant access pattern becomes: *insert many bid orders across a price range, then at auction time sweep from best price downward until the ask quantity is exhausted.*

---

### Revised Data Structures

```c
typedef long     Price;
typedef long     Qty;
typedef unsigned long OrderId;

typedef struct Order {
    OrderId      id;
    Price        price;
    Qty          qty;           /* remaining quantity */
    long         seq;           /* sequence number for time priority */
    struct Order *prev, *next;  /* intrusive FIFO list within level */
} Order;

typedef struct PriceLevel {
    Price        price;
    Qty          total_qty;     /* aggregate quantity at this level */
    Order       *head;          /* first-in (matched first) */
    Order       *tail;          /* last-in (appended here) */
} PriceLevel;

typedef struct AuctionBook {
    /*
     * O(1) price -> level lookup during order entry.
     * Only bid levels exist.
     */
    HashMap     *bid_levels;    /* Price -> PriceLevel* */

    /*
     * Sorted level index — built lazily at auction time.
     * An array is sufficient: we only need one full descending
     * sweep, so a sort-once / iterate-once pattern beats a tree.
     */
    PriceLevel **sorted_levels; /* descending price order, built at lock() */
    int          level_count;
    int          levels_dirty;  /* flag: set on insert, cleared after sort */

    /* Cached best bid for indicative price display — O(1) */
    PriceLevel  *best_bid;

    /* Order lookup for cancel/modify during open phase */
    HashMap     *order_map;     /* OrderId -> Order* */

    /* Auction state */
    int          is_locked;     /* 1 = no more order entry, auction imminent */
    Qty          total_bid_qty; /* running total across all levels */
} AuctionBook;
```

The `levels_dirty` flag means you only pay for the sort once, at lock-time, not on every insert.

---

### Core Operations

#### Add Bid Order

```c
void add_bid(AuctionBook *book, Order *order) {

    if (book->is_locked) return;   /* order entry closed */

    /* 1. Find or create price level — O(1) */
    PriceLevel *level = hashmap_get(book->bid_levels, order->price);
    if (level == NULL) {
        level = new_price_level(order->price);
        hashmap_put(book->bid_levels, order->price, level);
    }

    /* 2. Enqueue at tail — O(1) price-time priority */
    if (level->tail == NULL) {
        level->head = level->tail = order;
        order->prev = order->next = NULL;
    } else {
        order->prev       = level->tail;
        order->next       = NULL;
        level->tail->next = order;
        level->tail       = order;
    }
    level->total_qty      += order->qty;
    book->total_bid_qty   += order->qty;

    /* 3. Register in order map — O(1) */
    hashmap_put(book->order_map, order->id, order);

    /* 4. Update best bid — O(1) */
    if (book->best_bid == NULL || order->price > book->best_bid->price)
        book->best_bid = level;

    book->levels_dirty = 1;
}
```

#### Cancel Bid

```c
void cancel_bid(AuctionBook *book, OrderId id) {

    if (book->is_locked) return;

    Order *order = hashmap_get(book->order_map, id);
    if (order == NULL) return;

    PriceLevel *level = hashmap_get(book->bid_levels, order->price);

    /* Splice from FIFO list — O(1) */
    if (order->prev) order->prev->next = order->next;
    else             level->head       = order->next;

    if (order->next) order->next->prev = order->prev;
    else             level->tail       = order->prev;

    level->total_qty    -= order->qty;
    book->total_bid_qty -= order->qty;

    if (level->head == NULL) {
        hashmap_remove(book->bid_levels, order->price);
        if (book->best_bid == level)
            book->best_bid = NULL;  /* recomputed lazily or on next insert */
        free(level);
    }

    hashmap_remove(book->order_map, order->id);
    book->levels_dirty = 1;
    free(order);
}
```

#### Lock Book and Build Sorted Level Index

Called when order entry closes, just before the auction ask arrives. Pay the sort cost **once**.

```c
void lock_book(AuctionBook *book) {

    book->is_locked = 1;

    /* Collect all price levels into a flat array */
    book->level_count   = hashmap_count(book->bid_levels);
    book->sorted_levels = malloc(book->level_count * sizeof(PriceLevel*));

    HashMapIterator it = hashmap_iterator(book->bid_levels);
    int i = 0;
    while (hashmap_next(&it))
        book->sorted_levels[i++] = (PriceLevel*) it.value;

    /* Sort descending by price — O(L log L) where L = distinct price levels */
    qsort(book->sorted_levels, book->level_count,
          sizeof(PriceLevel*), compare_price_desc);
}
```

In practice L (distinct price levels) is far smaller than N (total orders), so this sort is cheap. For an equity auction with a tight price band you might have tens to hundreds of levels against thousands of orders.

#### Run Auction — Single Ask Sweep

```c
typedef struct FillResult {
    OrderId  order_id;
    Qty      filled_qty;
    Price    fill_price;
} FillResult;

void run_auction(AuctionBook *book, Qty ask_qty, Price ask_limit,
                 FillResult *fills_out, int *fill_count_out)
{
    int   fill_count    = 0;
    Qty   remaining_ask = ask_qty;

    /*
     * Sweep levels from best (highest) price downward.
     * Stop when ask is fully filled or no more levels are
     * at or above the ask limit.
     */
    for (int i = 0; i < book->level_count && remaining_ask > 0; i++) {

        PriceLevel *level = book->sorted_levels[i];

        if (level->price < ask_limit) break;  /* price floor not met */

        Order *order = level->head;
        while (order != NULL && remaining_ask > 0) {

            Qty fill_qty = MIN(order->qty, remaining_ask);

            fills_out[fill_count++] = (FillResult){
                .order_id  = order->id,
                .filled_qty = fill_qty,
                .fill_price = level->price   /* each level may clear at its own price,
                                                or use a single clearing price — see note */
            };

            order->qty    -= fill_qty;
            remaining_ask -= fill_qty;

            order = (order->qty == 0) ? order->next : order;
        }
    }

    *fill_count_out = fill_count;
}
```

> **Clearing price note:** In a uniform-price auction (most common for equities) all fills execute at a single clearing price — typically the lowest bid price that fully fills the ask. In a discriminatory-price auction each level clears at its own price. The sweep logic above supports both; just post-process `fills_out` to overwrite `fill_price` with the clearing price if needed.

---

### Complexity Summary

| Operation | Continuous LOB | Auction Book |
|---|---|---|
| Add order | O(1) / O(log n) new level | **O(1)** always |
| Cancel order | O(1) amortized | **O(1)** |
| Best bid lookup | O(1) cached | **O(1)** cached |
| Auction lock/sort | — | **O(L log L)**, L = price levels, once |
| Auction sweep | — | **O(N)**, N = filled orders |
| Memory | 2× bid+ask maps, 2× trees | **1× bid map, 1× flat array** |

---

### Design Notes

**No tree at all during order entry.** The RBTree was earning its keep in the continuous book by maintaining sorted order across millions of inserts and cancels. Here you sort once at lock-time — a flat array with `qsort` is faster, simpler, and more cache-friendly for the sequential sweep.

**Indicative auction price.** Before lock, you can cheaply compute the volume-maximizing clearing price by doing a preliminary sweep against a hypothetical ask. Since `total_bid_qty` is maintained incrementally, you know immediately if the ask can be fully filled without touching the levels.

**Memory pool still applies.** Pre-allocate `Order` and `PriceLevel` objects from a slab. For an auction you even know an upper bound on order count at system startup, so a fixed-size pool with zero runtime allocation is viable.
