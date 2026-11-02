package syntax

func IsNameStart(c byte) bool { return isIdentStart(c) }

func IsNameByte(c byte) bool { return isIdentByte(c) }

func IsDigit(c byte) bool { return isDigit(c) }
