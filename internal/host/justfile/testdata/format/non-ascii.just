set unstable
set lists

[arg('b', long='éé', help='y')]
[arg('a', long='abc', help='x')]
help a b:

[arg('d', short='d', help='éé', flag)]
[arg('c', short='c', help='abc', flag)]
flags c d:

[arg('f', short='f', help='öö', multiple)]
[arg('e', short='e', help='abc', multiple)]
multiple e f:
