package orchestration

type batchCounter struct{}

func (batchCounter) Count(s string) int { return len([]rune(s)) }
