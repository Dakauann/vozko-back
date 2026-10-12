package studio

var DefaultShadow = Shadow{Color: "#00000066", Blur: 12, Y: 6}

type TextShadowGuide struct {
	Color       string  `json:"color"`
	BlurShare   float64 `json:"blurShare"`
	OffsetShare float64 `json:"offsetShare"`
}

var TextShadow = TextShadowGuide{Color: "#00000059", BlurShare: 0.2, OffsetShare: 0.06}
