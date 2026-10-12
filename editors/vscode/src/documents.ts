const languageIds = ["shellscript", "bats", "makefile", "just", "nix"] as const;
const schemes = ["file", "untitled"] as const;

export const documentSelector = languageIds.flatMap((language) => schemes.map((scheme) => ({ language, scheme })));
