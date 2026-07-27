package acceptance

func stringSliceFromAny(value any) []string {
	if value == nil {
		return nil
	}
	if values, ok := value.([]string); ok {
		return values
	}
	if values, ok := value.([]any); ok {
		out := make([]string, 0, len(values))
		for _, item := range values {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}
