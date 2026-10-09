package lead

func IndexByNumber[T any](index map[string]T, number string, value T) {
	for _, format := range NumberFormats(number) {
		if _, taken := index[format]; !taken || format == number {
			index[format] = value
		}
	}
}

func FindByNumber[T any](index map[string]T, number string) (T, bool) {
	for _, format := range NumberFormats(number) {
		if value, ok := index[format]; ok {
			return value, true
		}
	}
	var zero T
	return zero, false
}
