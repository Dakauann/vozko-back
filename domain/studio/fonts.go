package studio

const (
	FontSans        = "sans"
	FontSerif       = "serif"
	FontDisplay     = "display"
	FontHandwriting = "handwriting"
	FontMono        = "mono"
)

var FontCategories = []string{FontSans, FontSerif, FontDisplay, FontHandwriting, FontMono}

var fontsByVoice = map[string][]string{
	FontSans: {
		"work-sans", "lato", "rubik", "manrope", "outfit", "space-grotesk", "plus-jakarta-sans", "josefin-sans",
		"raleway", "nunito", "open-sans", "roboto", "poppins", "montserrat", "dm-sans", "inter",
	},
	FontSerif: {"fraunces", "lora", "merriweather", "libre-baskerville", "eb-garamond", "cormorant-garamond", "dm-serif-display", "playfair-display"},
	FontDisplay: {
		"archivo-black", "alfa-slab-one", "abril-fatface", "staatliches", "oswald", "bebas-neue", "anton",
		"righteous", "fredoka", "titan-one", "bungee", "lobster", "creepster",
	},
	FontHandwriting: {"caveat", "kaushan-script", "shadows-into-light", "amatic-sc", "dancing-script", "sacramento", "great-vibes", "pacifico"},
	FontMono:        {"space-mono", "ibm-plex-mono", "dm-mono", "roboto-mono", "jetbrains-mono"},
}

var fontCategories = categoriesOf(fontsByVoice)

func categoriesOf(groups map[string][]string) map[string]string {
	out := map[string]string{}
	for category, ids := range groups {
		for _, id := range ids {
			out[id] = category
		}
	}
	return out
}

var fonts = fontSet()

func fontSet() map[string]bool {
	out := make(map[string]bool, len(fontCategories))
	for id := range fontCategories {
		out[id] = true
	}
	return out
}

func Fonts() []string {
	out := make([]string, 0, len(fontCategories))
	for id := range fontCategories {
		out = append(out, id)
	}
	return out
}

func FontCategory(id string) string {
	return fontCategories[id]
}

func FontsIn(category string) []string {
	return append([]string{}, fontsByVoice[category]...)
}

func OrderedFonts() []string {
	out := make([]string, 0, len(fontCategories))
	for _, category := range FontCategories {
		out = append(out, fontsByVoice[category]...)
	}
	return out
}
