package pagination

import "math"

// Page uses a bounded page size. Queries fetch one extra row using the same offset.
type Page[T any] struct {
	Items    []T  `json:"items"`
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	HasMore  bool `json:"has_more"`
}

func Normalize(page, size int) (int, int) {
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	if page < 1 {
		page = 1
	}
	if page > math.MaxInt/size {
		page = math.MaxInt / size
	}
	return page, size
}

func From[T any](items []T, page, size int) Page[T] {
	more := len(items) > size
	if more {
		items = items[:size]
	}
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Page: page, PageSize: size, HasMore: more}
}
