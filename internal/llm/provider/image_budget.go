package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	_ "golang.org/x/image/webp"
)

// Only image headers are decoded; pixels, paths and remote URLs are never read.
func imageBudgetProjection(model models.Model, history []message.Message, wire []byte) ([]byte, int64, error) {
	var reserve int64
	for _, msg := range history {
		if msg.Role != message.User {
			continue
		}
		for _, attachment := range msg.BinaryContent() {
			cfg, _, err := image.DecodeConfig(bytes.NewReader(attachment.Data))
			if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 65535 || cfg.Height > 65535 {
				return nil, 0, fmt.Errorf("cannot determine safe image dimensions for context budgeting")
			}
			reserve += imageTokenReserve(model, int64(cfg.Width), int64(cfg.Height))
		}
	}
	if reserve == 0 {
		return wire, 0, nil
	}
	var payload map[string]any
	if json.Unmarshal(wire, &payload) != nil {
		return nil, 0, fmt.Errorf("cannot project image context budget")
	}
	for _, field := range []string{"messages", "input", "contents"} {
		stripMessageImageData(payload[field])
	}
	text, err := json.Marshal(payload)
	return text, reserve, err
}

// OpenAI's documented high/auto tile rules, rounded upward. Patch models use
// unresized patch counts as a conservative upper bound instead of a billing cap.
// https://developers.openai.com/api/docs/guides/images-vision
func imageTokenReserve(model models.Model, width, height int64) int64 {
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderAzure {
		if reserve := openAIImageReserve(strings.ToLower(model.APIModel), width, height); reserve > 0 {
			return reserve
		}
	}
	if model.Provider == models.ProviderAnthropic || model.Provider == models.ProviderBedrock {
		// Cover both 28px patch models and older area-based estimates without
		// relying on provider-side downscaling. This is a reserve, not a bill.
		return max(patches(width, height, 28), (width*height+749)/750) + 128
	}
	// Unknown/local vision architectures keep a deliberately larger reserve;
	// never infer a compatible tokenizer just from an OpenAI-looking model name.
	return max(1024+4*patches(width, height, 14), tileImageReserve(width, height, 2833, 5667))
}

func patches(width, height, size int64) int64 {
	return ((width + size - 1) / size) * ((height + size - 1) / size)
}

func openAIImageReserve(model string, width, height int64) int64 {
	for _, rule := range []struct {
		prefix     string
		multiplier float64
	}{
		{"gpt-4.1-mini", 1.62}, {"gpt-4.1-nano", 2.46}, {"o4-mini", 1.72},
	} {
		if strings.HasPrefix(model, rule.prefix) {
			return int64(math.Ceil(float64(patches(width, height, 32))*rule.multiplier)) + 128
		}
	}
	base, tile := int64(0), int64(0)
	switch {
	case strings.HasPrefix(model, "gpt-4o-mini"):
		base, tile = 2833, 5667
	case strings.HasPrefix(model, "gpt-4o"), strings.HasPrefix(model, "gpt-4.1"), strings.HasPrefix(model, "gpt-4.5"):
		base, tile = 85, 170
	case strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"):
		base, tile = 75, 150
	default:
		return 0
	}
	return tileImageReserve(width, height, base, tile)
}

func tileImageReserve(width, height, base, tile int64) int64 {
	w, h := float64(width), float64(height)
	scale := min(1.0, 2048/max(w, h))
	w, h = w*scale, h*scale
	// Older high-detail endpoints can enlarge a small image to 768px on the
	// short edge. Reserving that larger interpretation also covers current APIs.
	scale = 768 / min(w, h)
	w, h = w*scale, h*scale
	return base + tile*patches(int64(math.Ceil(w)), int64(math.Ceil(h)), 512) + 128
}

func normalizeImageURLs(history []message.Message) ([]message.Message, error) {
	result := history
	cloned := false
	for i, msg := range history {
		next, changed, err := normalizeMessageImageURLs(msg)
		if err != nil {
			return nil, err
		}
		if changed {
			if !cloned {
				result = slices.Clone(history)
				cloned = true
			}
			result[i] = next
		}
	}
	return result, nil
}

func normalizeMessageImageURLs(msg message.Message) (message.Message, bool, error) {
	if msg.Role != message.User {
		return msg, false, nil
	}
	changed := false
	for i, part := range msg.Parts {
		if source, ok := part.(message.ImageURLContent); ok {
			attachment, err := decodeImageDataURL(source.URL)
			if err != nil {
				return msg, false, err
			}
			if !changed {
				msg.Parts = slices.Clone(msg.Parts)
				changed = true
			}
			msg.Parts[i] = attachment
		}
	}
	return msg, changed, nil
}

func decodeImageDataURL(value string) (message.BinaryContent, error) {
	if len(value) > maxProviderBytes {
		return message.BinaryContent{}, fmt.Errorf("image data URL exceeds 32 MiB")
	}
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
		return message.BinaryContent{}, fmt.Errorf("remote image URLs cannot be safely budgeted; attach image bytes instead")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return message.BinaryContent{}, fmt.Errorf("invalid image data URL; attach image bytes instead")
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return message.BinaryContent{}, fmt.Errorf("invalid image data URL; attach image bytes instead")
	}
	return message.BinaryContent{MIMEType: strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), Data: raw}, nil
}

// Traverse only real message-content slots. Tool schemas, tool arguments and
// text that happens to contain Base64 remain fully counted as text.
func stripMessageImageData(value any) {
	messages, _ := value.([]any)
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if images, ok := msg["images"].([]any); ok {
			for i := range images {
				images[i] = ""
			}
		}
		stripImageContentParts(msg["content"])
		stripImageContentParts(msg["parts"])
	}
}

func stripContentImageData(part map[string]any) {
	switch part["type"] {
	case "image_url":
		if source, ok := part["image_url"].(map[string]any); ok {
			source["url"] = ""
		}
	case "input_image":
		part["image_url"] = ""
	case "image":
		if source, ok := part["source"].(map[string]any); ok && source["type"] == "base64" {
			source["data"] = ""
		}
	}
	if source, ok := part["inlineData"].(map[string]any); ok {
		source["data"] = ""
	}
}

func stripImageContentParts(value any) {
	parts, _ := value.([]any)
	for _, raw := range parts {
		if part, ok := raw.(map[string]any); ok {
			stripContentImageData(part)
		}
	}
}
