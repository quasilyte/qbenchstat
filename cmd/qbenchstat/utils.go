package main

func colorizeText(str string, colorCode string) string {
	return colorCode + str + "\033[0m"
}

func redColorize(str string) string {
	return colorizeText(str, "\033[31m")
}

func greenColorize(str string) string {
	return colorizeText(str, "\033[32m")
}

func yellowColorize(str string) string {
	return colorizeText(str, "\033[33m")
}
