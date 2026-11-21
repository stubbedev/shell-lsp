set unstable
set lists
set default-script

[arg('target',long,help="00",value="")]
a target:

[arg('a', help="h", pattern='p', value="v", short='s', long='l')]
b a:

[arg('b', pattern='p', help="h", long, value="v", short)]
c b:

[arg('c', help="h", max='3', min='1', multiple, short, long='l')]
d c:

[arg('d', help='x', flag, short='t')]
e d:

[cache(outputs="o", inputs="i", extra="e")]
f:

[cache(inputs="i", extra="e")]
g:

[arg('e', long, max="10", multiple, min="2")]
h e:

[continue('SIGQUIT', "SIGHUP", 'SIGINT', 'SIGHUP')]
i:
