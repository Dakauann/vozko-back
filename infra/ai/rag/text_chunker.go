package rag

import (
	"strings"
	"unicode"

	"vozko/domain/rag"
)

type textChunker struct{}

func NewTextChunker() rag.TextChunker {
	return &textChunker{}
}

func (c *textChunker) Chunk(text string, config rag.ChunkingConfig) []rag.TextChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if config.Size <= 0 {
		config.Size = 512
	}
	if config.Overlap < 0 {
		config.Overlap = 0
	}

	switch config.Strategy {
	case rag.ChunkingStrategySentence:
		return c.chunkBySentence(text, config.Size, config.Overlap)
	case rag.ChunkingStrategyParagraph:
		return c.chunkByParagraph(text, config.Size)
	case rag.ChunkingStrategySemantic:
		return c.chunkBySemantic(text, config.Size, config.Overlap)
	default:
		return c.chunkByFixedSize(text, config.Size, config.Overlap)
	}
}

func (c *textChunker) chunkByFixedSize(text string, size int, overlap int) []rag.TextChunk {
	runes := []rune(text)
	totalLen := len(runes)

	if totalLen <= size {
		return []rag.TextChunk{{
			Content:     text,
			Index:       0,
			StartOffset: 0,
			EndOffset:   len(text),
		}}
	}

	chunks := make([]rag.TextChunk, 0)
	index := 0
	pos := 0

	for pos < totalLen {
		end := pos + size
		if end > totalLen {
			end = totalLen
		}

		endPos := end
		if end < totalLen {
			for endPos > pos && !unicode.IsSpace(runes[endPos-1]) {
				endPos--
			}
			if endPos == pos {
				endPos = end
			}
		}

		chunk := string(runes[pos:endPos])
		if strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, rag.TextChunk{
				Content:     strings.TrimSpace(chunk),
				Index:       index,
				StartOffset: pos,
				EndOffset:   endPos,
			})
			index++
		}

		nextPos := endPos - overlap
		if nextPos <= pos {
			nextPos = endPos
		}
		pos = nextPos
	}

	return chunks
}

func (c *textChunker) chunkBySentence(text string, maxSize int, overlap int) []rag.TextChunk {
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return nil
	}

	chunks := make([]rag.TextChunk, 0)
	var current strings.Builder
	currentStart := 0
	index := 0

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		if current.Len()+len(sentence)+1 > maxSize && current.Len() > 0 {
			cur := current.String()
			chunks = append(chunks, rag.TextChunk{
				Content:     strings.TrimSpace(cur),
				Index:       index,
				StartOffset: currentStart,
				EndOffset:   currentStart + len(cur),
			})
			index++

			overlapText := ""
			if overlap > 0 {
				runes := []rune(cur)
				if len(runes) > overlap {
					overlapText = string(runes[len(runes)-overlap:])
				}
			}

			current.Reset()
			currentStart += len(cur)

			if overlapText != "" {
				current.WriteString(overlapText)
			}
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}

	if current.Len() > 0 {
		chunks = append(chunks, rag.TextChunk{
			Content:     strings.TrimSpace(current.String()),
			Index:       index,
			StartOffset: currentStart,
			EndOffset:   currentStart + current.Len(),
		})
	}

	return chunks
}

func (c *textChunker) chunkByParagraph(text string, maxSize int) []rag.TextChunk {
	paragraphs := strings.Split(text, "\n\n")
	chunks := make([]rag.TextChunk, 0)
	var current strings.Builder
	currentStart := 0
	index := 0
	offset := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			offset += 2
			continue
		}

		if current.Len()+len(para)+2 > maxSize && current.Len() > 0 {
			chunks = append(chunks, rag.TextChunk{
				Content:     strings.TrimSpace(current.String()),
				Index:       index,
				StartOffset: currentStart,
				EndOffset:   offset,
			})
			index++
			current.Reset()
			currentStart = offset
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
		offset += len(para) + 2
	}

	if current.Len() > 0 {
		chunks = append(chunks, rag.TextChunk{
			Content:     strings.TrimSpace(current.String()),
			Index:       index,
			StartOffset: currentStart,
			EndOffset:   offset,
		})
	}

	return chunks
}

func (c *textChunker) chunkBySemantic(text string, maxSize int, overlap int) []rag.TextChunk {
	return c.chunkBySentence(text, maxSize, overlap)
}

func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		if r == '.' || r == '!' || r == '?' {
			if i+1 < len(runes) && (unicode.IsSpace(runes[i+1]) || runes[i+1] == '\n') {
				s := strings.TrimSpace(current.String())
				if s != "" {
					sentences = append(sentences, s)
				}
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		if s != "" {
			sentences = append(sentences, s)
		}
	}

	return sentences
}
