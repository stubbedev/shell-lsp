set unstable
set lists

[arg('d', short='w', flag)]
[arg('c', short='z', flag)]
[arg('b', short='y', multiple)]
[arg('a', short='x', multiple)]
flags a b c d:

[arg('bb', help='y')]
[arg('a', help    ='x')]
[arg('c', long)]
help a bb c:

[arg('a', short='x', multiple, max='10')]
[arg('b', short='y', multiple, max='9')]
[arg('c', short='z', multiple, min='1')]
[arg('d', short='w', multiple)]
counts a b c d:

[arg('b', short='x', multiple, max='3')]
[arg('a', short='y',  multiple, max='3')]
[arg('d', short='w', multiple, min='2')]
[arg('c', short='z',  multiple, min='2')]
keys a b c d:

[arg('b', short='x', flag, help='h')]
[arg('a', long, help='h')]
[arg('c', long='cc', short='c')]
[arg('e', short='e', value='v')]
[arg('d', short='d')]
mixed a b c d e:
