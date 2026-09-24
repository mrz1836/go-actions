package actions

// ClampLimit normalizes a requested page size into [1, ceiling], returning def
// when limit is non-positive. It centralizes the "default then cap" policy every
// list endpoint repeats, with the bounds supplied by the caller rather than
// hardcoded so each surface keeps its own page-size policy.
func ClampLimit(limit, def, ceiling int) int {
	switch {
	case limit <= 0:
		return def
	case limit > ceiling:
		return ceiling
	default:
		return limit
	}
}

// Paginate trims a fetched slice to one page and builds a Page envelope. The
// convention is to fetch limit+1 rows: a present extra row means another page
// exists, so Paginate drops it, sets HasMore, and derives NextCursor from the
// last kept row via cursorOf. A nil Items slice is normalized to an empty slice
// so the body encodes "items": [] rather than null.
//
// Cursor encoding is entirely the caller's concern — cursorOf returns whatever
// opaque string the surface uses — so this helper stays dependency-free and does
// not dictate a cursor format. A non-positive limit disables trimming and returns
// every row as a single page.
func Paginate[T any](rows []T, limit int, cursorOf func(last T) string) Page[T] {
	page := Page[T]{}

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
		page.HasMore = true
		page.NextCursor = cursorOf(rows[len(rows)-1])
	}

	if rows == nil {
		rows = []T{}
	}
	page.Items = rows

	return page
}
