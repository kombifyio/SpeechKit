package store

const defaultListLimit = 20

func normalizedListPagination(opts ListOpts) (int, int) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
