# frozen_string_literal: true

module GithubMarkdown
  REPOSITORY = "https://github.com/stubbedev/shell-lsp/blob/main/"
  ALERT = /^> \[!(NOTE|TIP)\]\s*$/
  DETAILS = /^<details>\s*$/
  SUMMARY = /^<summary>/
  CODE_OR_LINK = /`[^`]*`|\]\((?![a-z]+:|#|\/)([^)\s]+)\)/

  module_function

  def repository_link(page, target)
    path, anchor = target.split("#", 2)
    resolved = File.expand_path(path, File.join("/", File.dirname(page.relative_path))).delete_prefix("/")
    "](#{REPOSITORY}#{resolved}#{anchor ? "##{anchor}" : ""})"
  end

  def convert(page)
    fenced = false
    page.content = page.content.each_line.map do |line|
      fenced = !fenced if line.start_with?("```")
      next line if fenced || line.start_with?("```")

      line
        .sub(ALERT) { "{: .#{Regexp.last_match(1).downcase} }\n" }
        .sub(DETAILS, %(<details markdown="block">\n))
        .sub(SUMMARY, %(<summary markdown="span">))
        .gsub(CODE_OR_LINK) { |match| match.start_with?("`") ? match : repository_link(page, Regexp.last_match(1)) }
    end.join
  end
end

Jekyll::Hooks.register(:pages, :pre_render) do |page|
  GithubMarkdown.convert(page) if page.relative_path.end_with?(".md")
end
