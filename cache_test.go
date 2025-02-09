// Copyright (c) 2023 Alexey Mayshev. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otter

import (
	"container/heap"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/maypok86/otter/internal/xruntime"
)

func TestCache_Set(t *testing.T) {
	const size = 100
	c, err := MustBuilder[int, int](size).WithTTL(time.Minute).CollectStats().Build()
	if err != nil {
		t.Fatalf("can not create cache: %v", err)
	}

	for i := 0; i < size; i++ {
		c.Set(i, i)
	}

	// update
	for i := 0; i < size; i++ {
		c.Set(i, i)
	}

	parallelism := xruntime.Parallelism()
	var wg sync.WaitGroup
	for i := 0; i < int(parallelism); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			for a := 0; a < 10000; a++ {
				k := r.Int() % 100
				val, ok := c.Get(k)
				if !ok {
					err = fmt.Errorf("expected %d but got nil", k)
					break
				}
				if val != k {
					err = fmt.Errorf("expected %d but got %d", k, val)
					break
				}
			}
		}()
	}
	wg.Wait()

	if err != nil {
		t.Fatalf("not found key: %v", err)
	}
	ratio := c.Stats().Ratio()
	if ratio != 1.0 {
		t.Fatalf("cache hit ratio should be 1.0, but got %v", ratio)
	}
}

func TestCache_SetIfAbsent(t *testing.T) {
	const size = 100
	c, err := MustBuilder[int, int](size).WithTTL(time.Minute).CollectStats().Build()
	if err != nil {
		t.Fatalf("can not create cache: %v", err)
	}

	for i := 0; i < size; i++ {
		if !c.SetIfAbsent(i, i) {
			t.Fatalf("set was dropped. key: %d", i)
		}
	}

	for i := 0; i < size; i++ {
		if !c.Has(i) {
			t.Fatalf("key should exists: %d", i)
		}
	}

	for i := 0; i < size; i++ {
		if c.SetIfAbsent(i, i) {
			t.Fatalf("set wasn't dropped. key: %d", i)
		}
	}

	c.Clear()

	cc, err := MustBuilder[int, int](size).WithVariableTTL().CollectStats().Build()
	if err != nil {
		t.Fatalf("can not create cache: %v", err)
	}

	for i := 0; i < size; i++ {
		if !cc.SetIfAbsent(i, i, time.Hour) {
			t.Fatalf("set was dropped. key: %d", i)
		}
	}

	for i := 0; i < size; i++ {
		if !cc.Has(i) {
			t.Fatalf("key should exists: %d", i)
		}
	}

	for i := 0; i < size; i++ {
		if cc.SetIfAbsent(i, i, time.Second) {
			t.Fatalf("set wasn't dropped. key: %d", i)
		}
	}

	if hits := cc.Stats().Hits(); hits != size {
		t.Fatalf("hit ratio should be 100%%. Hits: %d", hits)
	}

	cc.Close()
}

func TestCache_SetWithTTL(t *testing.T) {
	size := 256
	c, err := MustBuilder[int, int](size).
		InitialCapacity(size).
		WithTTL(time.Second).
		Build()
	if err != nil {
		t.Fatalf("can not create builder: %v", err)
	}

	for i := 0; i < size; i++ {
		c.Set(i, i)
	}

	time.Sleep(3 * time.Second)
	for i := 0; i < size; i++ {
		if c.Has(i) {
			t.Fatalf("key should be expired: %d", i)
		}
	}

	time.Sleep(10 * time.Millisecond)

	if cacheSize := c.Size(); cacheSize != 0 {
		t.Fatalf("c.Size() = %d, want = %d", cacheSize, 0)
	}

	cc, err := MustBuilder[int, int](size).WithVariableTTL().CollectStats().Build()
	if err != nil {
		t.Fatalf("can not create builder: %v", err)
	}

	for i := 0; i < size; i++ {
		cc.Set(i, i, 5*time.Second)
	}

	time.Sleep(7 * time.Second)

	for i := 0; i < size; i++ {
		if cc.Has(i) {
			t.Fatalf("key should be expired: %d", i)
		}
	}

	time.Sleep(10 * time.Millisecond)

	if cacheSize := cc.Size(); cacheSize != 0 {
		t.Fatalf("c.Size() = %d, want = %d", cacheSize, 0)
	}
	if misses := cc.Stats().Misses(); misses != int64(size) {
		t.Fatalf("c.Stats().Misses() = %d, want = %d", misses, size)
	}
}

func TestBaseCache_DeleteByFunc(t *testing.T) {
	size := 256
	c, err := MustBuilder[int, int](size).
		InitialCapacity(size).
		WithTTL(time.Hour).
		Build()
	if err != nil {
		t.Fatalf("can not create builder: %v", err)
	}

	for i := 0; i < size; i++ {
		c.Set(i, i)
	}

	c.DeleteByFunc(func(key int, value int) bool {
		return key%2 == 1
	})

	c.Range(func(key int, value int) bool {
		if key%2 == 1 {
			t.Fatalf("key should be odd, but got: %d", key)
		}
		return true
	})
}

func TestCache_Ratio(t *testing.T) {
	c, err := MustBuilder[uint64, uint64](100).CollectStats().Build()
	if err != nil {
		t.Fatalf("can not create cache: %v", err)
	}

	z := rand.NewZipf(rand.New(rand.NewSource(time.Now().UnixNano())), 1.0001, 1, 1000)

	o := newOptimal(100)
	for i := 0; i < 10000; i++ {
		k := z.Uint64()

		o.Get(k)
		if !c.Has(k) {
			c.Set(k, k)
		}
	}

	t.Logf("actual size: %d, capacity: %d", c.Size(), c.Capacity())
	t.Logf("actual: %.2f, optimal: %.2f", c.Stats().Ratio(), o.Ratio())
}

type optimal struct {
	capacity uint64
	hits     map[uint64]uint64
	access   []uint64
}

func newOptimal(capacity uint64) *optimal {
	return &optimal{
		capacity: capacity,
		hits:     make(map[uint64]uint64),
		access:   make([]uint64, 0),
	}
}

func (o *optimal) Get(key uint64) {
	o.hits[key]++
	o.access = append(o.access, key)
}

func (o *optimal) Ratio() float64 {
	look := make(map[uint64]struct{}, o.capacity)
	data := &optimalHeap{}
	heap.Init(data)
	hits := 0
	misses := 0
	for _, key := range o.access {
		if _, has := look[key]; has {
			hits++
			continue
		}
		if uint64(data.Len()) >= o.capacity {
			victim := heap.Pop(data)
			delete(look, victim.(*optimalItem).key)
		}
		misses++
		look[key] = struct{}{}
		heap.Push(data, &optimalItem{key, o.hits[key]})
	}
	if hits == 0 && misses == 0 {
		return 0.0
	}
	return float64(hits) / float64(hits+misses)
}

type optimalItem struct {
	key  uint64
	hits uint64
}

type optimalHeap []*optimalItem

func (h optimalHeap) Len() int           { return len(h) }
func (h optimalHeap) Less(i, j int) bool { return h[i].hits < h[j].hits }
func (h optimalHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *optimalHeap) Push(x any) {
	*h = append(*h, x.(*optimalItem))
}

func (h *optimalHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
