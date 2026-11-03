# Shell findings in just and Make recipes

Written by `just corpus-baseline` from the justfiles and Makefiles of the repositories in
[repos.txt](repos.txt); see [docs/corpus.md](../docs/corpus.md). Positions and codes only, one
finding a line. CI fails when anything here changes.

169 files checked, 453 findings.

| Code | Findings |
| --- | ---: |
| SC1001 | 12 |
| SC1007 | 1 |
| SC1083 | 1 |
| SC2004 | 1 |
| SC2006 | 34 |
| SC2009 | 6 |
| SC2011 | 1 |
| SC2015 | 6 |
| SC2016 | 17 |
| SC2028 | 3 |
| SC2034 | 6 |
| SC2035 | 69 |
| SC2038 | 11 |
| SC2044 | 1 |
| SC2046 | 13 |
| SC2059 | 5 |
| SC2086 | 139 |
| SC2091 | 1 |
| SC2092 | 2 |
| SC2094 | 8 |
| SC2115 | 1 |
| SC2121 | 1 |
| SC2128 | 1 |
| SC2153 | 1 |
| SC2162 | 2 |
| SC2164 | 79 |
| SC2166 | 5 |
| SC2181 | 1 |
| SC2196 | 1 |
| SC2206 | 4 |
| SC2251 | 1 |
| SC2254 | 5 |
| SC2268 | 2 |
| SC2336 | 2 |
| SC3003 | 4 |
| SC3009 | 1 |
| SC3037 | 1 |
| SL2016 | 1 |
| SL2020 | 2 |
| SL3002 | 1 |

## Findings

bluefin/Justfile:33:45 SC2086
bluefin/Justfile:43:28 SL2016
bluefin/Justfile:51:37 SC2086
bluefin/Justfile:62:10 SC2035
bluefin/Justfile:176:63 SC2086
bluefin/Justfile:179:15 SC2091
bluefin/Justfile:193:9 SC2034
bluefin/Justfile:224:71 SC1001
bluefin/Justfile:224:75 SC1001
bluefin/Justfile:224:79 SC1001
bluefin/Justfile:224:83 SC1001
bluefin/Justfile:224:87 SC1001
bluefin/Justfile:224:91 SC1001
bluefin/Justfile:301:20 SC2086
bluefin/Justfile:304:26 SC2086
bluefin/Justfile:304:37 SC2086
bluefin/Justfile:309:22 SC2086
bluefin/Justfile:310:27 SC2086
bluefin/Justfile:310:45 SC2086
bluefin/Justfile:312:23 SC2086
bluefin/Justfile:315:32 SC2086
bluefin/Justfile:315:50 SC2086
bluefin/Justfile:318:25 SC2086
bluefin/Justfile:318:43 SC2086
bluefin/Justfile:333:55 SC1001
bluefin/Justfile:333:59 SC1001
bluefin/Justfile:333:63 SC1001
bluefin/Justfile:333:67 SC1001
bluefin/Justfile:333:71 SC1001
bluefin/Justfile:333:75 SC1001
bluefin/Justfile:339:34 SC2086
bluefin/Justfile:339:52 SC2086
bluefin/Justfile:349:24 SC2086
bluefin/Justfile:349:113 SC2086
bluefin/Justfile:362:15 SC2086
bluefin/Justfile:363:16 SC2086
bluefin/Justfile:375:15 SC2086
bluefin/Justfile:387:15 SC2086
bluefin/Justfile:388:15 SC2086
bluefin/Justfile:389:15 SC2086
bluefin/Justfile:395:15 SC2086
bluefin/Justfile:396:16 SC2086
bluefin/Justfile:416:20 SC2086
bluefin/Justfile:417:20 SC2086
bluefin/Justfile:420:33 SC2128
bluefin/Justfile:422:30 SC2153
bluefin/Justfile:426:15 SC2086
bluefin/Justfile:451:19 SC2086
bluefin/Justfile:493:41 SC2086
bluefin/Justfile:494:19 SC2086
bluefin/Justfile:495:19 SC2086
bluefin/Justfile:528:40 SC2086
bluefin/Justfile:528:49 SC2086
bluefin/Justfile:528:56 SC2086
bluefin/Justfile:616:5 SC2034
bluefin/Justfile:617:5 SC2034
bluefin/Justfile:632:26 SC2206
bluefin/Justfile:632:55 SC2206
bluefin/Justfile:633:23 SC2206
bluefin/Justfile:633:46 SC2206
bluefin/Justfile:692:23 SC2086
bluefin/Justfile:692:47 SC2086
bluefin/Justfile:712:49 SC2086
bluefin/Justfile:717:22 SC2086
bluefin/Justfile:718:28 SC2086
bluefin/Justfile:722:80 SC2086
bluefin/Justfile:768:46 SC2086
buildroot/package/pkg-generic.mk:180:33 SC2006
buildroot/package/pkg-generic.mk:180:33 SC2046
buildroot/package/pkg-generic.mk:180:43 SC2086
buildroot/package/pkg-generic.mk:321:8 SC2016
buildroot/package/pkg-generic.mk:322:8 SC2016
buildroot/package/pkg-generic.mk:327:12 SC2044
ffmpeg/Makefile:119:11 SC2059
git-make/gitweb/Makefile:108:17 SC2015
just/justfile:86:3 SC2251
just/justfile:87:11 SC2006
just/justfile:88:14 SC2086
just/justfile:89:19 SC2086
just/justfile:232:28 SC2006
linux-tools/tools/lib/api/Makefile:121:2 SC2038
linux-tools/tools/lib/bpf/Makefile:264:3 SC2035
linux-tools/tools/lib/bpf/Makefile:273:5 SC2035
linux-tools/tools/lib/bpf/Makefile:273:9 SC2035
linux-tools/tools/lib/bpf/Makefile:278:2 SC2011
linux-tools/tools/lib/bpf/Makefile:278:5 SC2035
linux-tools/tools/lib/bpf/Makefile:278:9 SC2035
linux-tools/tools/lib/perf/Makefile:122:17 SC2035
linux-tools/tools/lib/perf/Makefile:122:21 SC2035
linux-tools/tools/lib/perf/Makefile:122:24 SC2035
linux-tools/tools/lib/perf/Makefile:122:28 SC2035
linux-tools/tools/lib/perf/Makefile:122:33 SC2035
linux-tools/tools/lib/perf/Makefile:122:49 SC2035
linux-tools/tools/lib/subcmd/Makefile:112:2 SC2038
linux-tools/tools/lib/symbol/Makefile:110:2 SC2038
linux-tools/tools/lib/thermal/Makefile:116:17 SC2035
linux-tools/tools/lib/thermal/Makefile:116:21 SC2035
linux-tools/tools/lib/thermal/Makefile:116:24 SC2035
linux-tools/tools/lib/thermal/Makefile:116:28 SC2035
linux-tools/tools/lib/thermal/Makefile:116:33 SC2035
linux-tools/tools/lib/thermal/Makefile:116:49 SC2035
linux-tools/tools/testing/selftests/Makefile:231:9 SC2086
linux-tools/tools/testing/selftests/Makefile:232:18 SC2086
linux-tools/tools/testing/selftests/Makefile:232:36 SC2086
linux-tools/tools/testing/selftests/Makefile:241:18 SC2086
linux-tools/tools/testing/selftests/Makefile:241:36 SC2086
linux-tools/tools/testing/selftests/Makefile:250:18 SC2086
linux-tools/tools/testing/selftests/Makefile:250:36 SC2086
linux-tools/tools/testing/selftests/Makefile:256:18 SC2086
linux-tools/tools/testing/selftests/Makefile:256:36 SC2086
linux-tools/tools/testing/selftests/Makefile:262:18 SC2086
linux-tools/tools/testing/selftests/Makefile:262:36 SC2086
linux-tools/tools/testing/selftests/Makefile:296:18 SC2086
linux-tools/tools/testing/selftests/Makefile:296:36 SC2086
linux-tools/tools/testing/selftests/Makefile:297:34 SC2086
linux-tools/tools/testing/selftests/Makefile:312:26 SC2086
linux-tools/tools/testing/selftests/Makefile:312:47 SC2059
linux-tools/tools/testing/selftests/Makefile:313:10 SC2059
linux-tools/tools/testing/selftests/Makefile:314:42 SC2086
linux-tools/tools/testing/selftests/Makefile:314:68 SC2086
linux-tools/tools/testing/selftests/Makefile:315:7 SC2086
linux-tools/tools/testing/selftests/Makefile:320:10 SC2059
linux-tools/tools/testing/selftests/Makefile:339:18 SC2086
linux-tools/tools/testing/selftests/Makefile:339:36 SC2086
linux-tools/tools/testing/selftests/lib.mk:131:8 SC2268
linux-tools/tools/testing/selftests/lib.mk:135:8 SC2268
linux-tools/tools/testing/selftests/lib.mk:183:17 SC2006
linux-tools/tools/testing/selftests/lib.mk:183:27 SC2086
luajit/Makefile:154:35 SC2086
luajit/Makefile:157:32 SC2086
luajit/src/Makefile:648:12 SC2086
luajit/src/Makefile:648:28 SC2086
luajit/src/Makefile:650:34 SC2035
luajit/src/Makefile:657:12 SC2086
luajit/src/Makefile:657:33 SC2086
micropython/ports/unix/Makefile:300:2 SC2164
micropython/ports/unix/Makefile:305:32 SC2164
micropython/ports/unix/Makefile:306:72 SC2086
micropython/py/manifest.mk:52:50 SC3037
micropython/py/mkrules.mk:255:14 SC2046
micropython/py/mkrules.mk:274:16 SC2015
moby-make/Makefile:260:20 SC2035
moby-make/Makefile:281:9 SC2115
node/Makefile:424:5 SC2181
node/Makefile:592:2 SC2009
node/Makefile:593:10 SC2006
node/Makefile:593:11 SC2009
node/Makefile:595:8 SC2086
node/Makefile:654:2 SC2009
node/Makefile:655:10 SC2006
node/Makefile:655:11 SC2009
node/Makefile:657:8 SC2086
node/Makefile:669:2 SC2009
node/Makefile:670:10 SC2006
node/Makefile:670:11 SC2009
node/Makefile:672:8 SC2086
node/Makefile:892:5 SC2336
node/Makefile:1156:39 SC2006
node/Makefile:1156:39 SC2092
node/Makefile:1157:8 SC2028
node/Makefile:1159:8 SC2028
node/Makefile:1163:3 SC2006
node/Makefile:1163:3 SC2092
node/Makefile:1174:28 SC2196
node/Makefile:1206:20 SC2086
node/Makefile:1207:60 SC2086
node/Makefile:1208:10 SC2059
node/Makefile:1209:7 SC2086
node/Makefile:1212:52 SC2086
node/Makefile:1213:7 SC2086
node/Makefile:1216:52 SC2086
node/Makefile:1283:5 SC2336
node/Makefile:1339:2 SC2038
node/Makefile:1340:2 SC2038
node/Makefile:1341:2 SC2038
node/Makefile:1342:2 SC2038
node/Makefile:1343:2 SC2038
node/Makefile:1388:2 SC2038
node/Makefile:1510:10 SC2016
ord/justfile:24:12 SL2020
ord/justfile:73:48 SC2086
ord/justfile:74:24 SC2086
ord/justfile:82:35 SC2086
ord/justfile:89:14 SC2086
ord/justfile:121:11 SC2006
ord/justfile:123:27 SC2086
ord/justfile:146:11 SC2006
ord/justfile:147:14 SC2086
ord/justfile:148:44 SC2086
ord/justfile:170:15 SC2086
ord/justfile:173:20 SC2086
postgres/src/makefiles/pgxs.mk:470:6 SC2016
prometheus/Makefile:231:64 SC2086
redis/Makefile:91:39 SC2086
redis/src/Makefile:507:18 SC2016
redis/src/Makefile:515:143 SC2035
redis/src/Makefile:515:147 SC2035
redis/src/Makefile:515:154 SC2035
redis/src/Makefile:515:161 SC2035
redis/src/Makefile:515:202 SC2035
redis/src/Makefile:583:23 SC3009
ruby/common.mk:690:27 SC2121
ruby/common.mk:710:29 SC2015
ruby/common.mk:712:29 SC2015
ruby/common.mk:733:23 SC2035
ruby/common.mk:733:41 SC2035
ruby/common.mk:733:49 SC2035
ruby/common.mk:735:12 SC2035
ruby/common.mk:735:15 SC2035
ruby/common.mk:735:21 SC2035
ruby/common.mk:735:38 SC2035
ruby/common.mk:764:25 SC2015
ruby/common.mk:1354:19 SC2034
ruby/common.mk:1354:29 SC1007
ruby/common.mk:1354:30 SC2034
ruby/common.mk:1358:29 SC2015
ruby/common.mk:1483:7 SC2016
ruby/common.mk:1485:7 SC2016
ruby/common.mk:1567:9 SC2016
ruby/common.mk:1582:9 SC2016
ruby/common.mk:1994:5 SC2016
ruby/defs/gmake.mk:213:2 SC1083
ruby/defs/gmake.mk:579:5 SC2016
ruby/defs/gmake.mk:580:5 SC2016
ruby/defs/gmake.mk:582:5 SC2016
ruby/defs/gmake.mk:583:5 SC2016
ruby/defs/gmake.mk:605:10 SC3003
ruby/defs/gmake.mk:605:31 SC3003
ruby/defs/gmake.mk:605:60 SC3003
ruby/defs/gmake.mk:605:88 SC3003
u-boot/Makefile:364:34 SC2086
u-boot/Makefile:634:33 SC2166
u-boot/Makefile:635:33 SC2166
u-boot/Makefile:1604:11 SC2086
u-boot/Makefile:1604:26 SC2086
u-boot/Makefile:1636:20 SC2004
u-boot/Makefile:1637:7 SC2086
u-boot/Makefile:2273:33 SC2166
u-boot/Makefile:2496:21 SC2006
u-boot/Makefile:2496:21 SC2046
u-boot/Makefile:2501:21 SC2006
u-boot/Makefile:2501:21 SC2046
u-boot/Makefile:2521:10 SC2006
u-boot/Makefile:2613:3 SC2038
u-boot/Makefile:2629:3 SC2038
u-boot/Makefile:2662:10 SC2016
u-boot/Makefile:2664:8 SC2164
u-boot/Makefile:2665:67 SC2086
u-boot/Makefile:2667:68 SC2086
u-boot/Makefile:2668:32 SC2086
u-boot/Makefile:2669:11 SC2086
u-boot/Makefile:2669:18 SC2086
u-boot/Makefile:2671:9 SC2028
u-boot/Makefile:2674:13 SC2086
u-boot/Makefile:2674:20 SC2086
u-boot/Makefile:2677:13 SC2086
u-boot/Makefile:2677:20 SC2086
u-boot/Makefile:2680:7 SC2086
u-boot/Makefile:2680:14 SC2086
u-boot/Makefile:2682:23 SC2162
u-boot/Makefile:2682:54 SC2162
u-boot/Makefile:2682:59 SC2034
u-boot/Makefile:2700:4 SC2006
u-boot/Makefile:2701:27 SC2006
u-boot/Makefile:2701:27 SC2046
u-boot/Makefile:2701:69 SC2086
u-boot/Makefile:2765:9 SC2016
u-boot/Makefile:2766:9 SC2016
u-boot/Makefile:2802:23 SC2046
vim/src/Makefile:1656:2 SC2164
vim/src/Makefile:2140:2 SC2164
vim/src/Makefile:2147:3 SC2164
vim/src/Makefile:2151:3 SC2164
vim/src/Makefile:2157:2 SC2164
vim/src/Makefile:2252:3 SC2164
vim/src/Makefile:2257:2 SC2164
vim/src/Makefile:2261:2 SC2164
vim/src/Makefile:2264:2 SC2164
vim/src/Makefile:2268:2 SC2164
vim/src/Makefile:2300:6 SC2164
vim/src/Makefile:2307:2 SC2164
vim/src/Makefile:2316:2 SC2164
vim/src/Makefile:2319:2 SC2164
vim/src/Makefile:2326:2 SC2164
vim/src/Makefile:2328:3 SC2164
vim/src/Makefile:2417:2 SC2164
vim/src/Makefile:2420:14 SC2006
vim/src/Makefile:2420:14 SL2020
vim/src/Makefile:2420:21 SC2164
vim/src/Makefile:2422:2 SC2164
vim/src/Makefile:2423:9 SC2006
vim/src/Makefile:2423:13 SC2035
vim/src/Makefile:2424:18 SC2006
vim/src/Makefile:2424:22 SC2035
vim/src/Makefile:2425:19 SC2086
vim/src/Makefile:2426:3 SC2164
vim/src/Makefile:2427:20 SC2086
vim/src/Makefile:2430:2 SC2164
vim/src/Makefile:2479:2 SC2164
vim/src/Makefile:2479:37 SC2035
vim/src/Makefile:2480:2 SC2164
vim/src/Makefile:2480:37 SC2035
vim/src/Makefile:2482:2 SC2164
vim/src/Makefile:2482:37 SC2035
vim/src/Makefile:2483:2 SC2164
vim/src/Makefile:2484:2 SC2164
vim/src/Makefile:2484:35 SC2035
vim/src/Makefile:2486:2 SC2164
vim/src/Makefile:2486:35 SC2035
vim/src/Makefile:2487:2 SC2164
vim/src/Makefile:2487:35 SC2035
vim/src/Makefile:2488:2 SC2164
vim/src/Makefile:2488:42 SC2035
vim/src/Makefile:2489:2 SC2164
vim/src/Makefile:2489:42 SC2035
vim/src/Makefile:2490:2 SC2164
vim/src/Makefile:2490:47 SC2035
vim/src/Makefile:2491:2 SC2164
vim/src/Makefile:2491:47 SC2035
vim/src/Makefile:2493:2 SC2164
vim/src/Makefile:2493:35 SC2035
vim/src/Makefile:2494:2 SC2164
vim/src/Makefile:2494:35 SC2035
vim/src/Makefile:2496:2 SC2164
vim/src/Makefile:2496:36 SC2035
vim/src/Makefile:2497:2 SC2164
vim/src/Makefile:2497:36 SC2035
vim/src/Makefile:2498:2 SC2164
vim/src/Makefile:2498:41 SC2035
vim/src/Makefile:2499:2 SC2164
vim/src/Makefile:2499:41 SC2035
vim/src/Makefile:2500:2 SC2164
vim/src/Makefile:2500:40 SC2035
vim/src/Makefile:2501:2 SC2164
vim/src/Makefile:2501:40 SC2035
vim/src/Makefile:2502:2 SC2164
vim/src/Makefile:2502:42 SC2035
vim/src/Makefile:2503:2 SC2164
vim/src/Makefile:2503:42 SC2035
vim/src/Makefile:2504:2 SC2164
vim/src/Makefile:2504:41 SC2035
vim/src/Makefile:2505:2 SC2164
vim/src/Makefile:2505:41 SC2035
vim/src/Makefile:2507:2 SC2164
vim/src/Makefile:2507:43 SC2035
vim/src/Makefile:2508:2 SC2164
vim/src/Makefile:2508:43 SC2035
vim/src/Makefile:2511:2 SC2164
vim/src/Makefile:2511:36 SC2035
vim/src/Makefile:2512:2 SC2164
vim/src/Makefile:2512:36 SC2035
vim/src/Makefile:2514:2 SC2164
vim/src/Makefile:2514:38 SC2035
vim/src/Makefile:2515:2 SC2164
vim/src/Makefile:2515:35 SC2035
vim/src/Makefile:2517:2 SC2164
vim/src/Makefile:2517:36 SC2035
vim/src/Makefile:2518:2 SC2164
vim/src/Makefile:2518:36 SC2035
vim/src/Makefile:2522:18 SC2006
vim/src/Makefile:2522:18 SC2046
vim/src/Makefile:2523:19 SC2006
vim/src/Makefile:2523:19 SC2046
vim/src/Makefile:2527:6 SC2006
vim/src/Makefile:2529:11 SC2086
vim/src/Makefile:2534:18 SC2006
vim/src/Makefile:2534:18 SC2046
vim/src/Makefile:2535:19 SC2006
vim/src/Makefile:2535:19 SC2046
vim/src/Makefile:2585:6 SC2006
vim/src/Makefile:2587:11 SC2086
vim/src/Makefile:2591:11 SC2006
vim/src/Makefile:2591:69 SC2094
vim/src/Makefile:2591:97 SC2094
vim/src/Makefile:2592:10 SC2006
vim/src/Makefile:2592:67 SC2094
vim/src/Makefile:2592:91 SC2094
vim/src/Makefile:2593:11 SC2006
vim/src/Makefile:2593:68 SC2094
vim/src/Makefile:2593:92 SC2094
vim/src/Makefile:2594:11 SC2006
vim/src/Makefile:2594:67 SC2094
vim/src/Makefile:2594:91 SC2094
vim/src/Makefile:2595:22 SC2006
vim/src/Makefile:2595:22 SC2046
vim/src/Makefile:2700:5 SC2164
vim/src/Makefile:2721:13 SC2006
vim/src/Makefile:2721:19 SC2086
vim/src/Makefile:2725:37 SC2086
vim/src/Makefile:2726:44 SC2086
vim/src/Makefile:2727:41 SC2086
vim/src/Makefile:2729:38 SC2086
vim/src/Makefile:2730:45 SC2086
vim/src/Makefile:2731:42 SC2086
vim/src/Makefile:2822:2 SC2164
vim/src/Makefile:2825:2 SC2164
vim/src/Makefile:2828:2 SC2164
vim/src/Makefile:2831:2 SC2164
vim/src/Makefile:2834:2 SC2164
vim/src/Makefile:2837:2 SC2164
vim/src/Makefile:2840:2 SC2164
vim/src/Makefile:2843:2 SC2164
vim/src/Makefile:2846:2 SC2164
vim/src/Makefile:2849:2 SC2164
vim/src/Makefile:2852:2 SC2164
vim/src/Makefile:2855:2 SC2164
vim/src/Makefile:3021:9 SC2035
vim/src/Makefile:3024:19 SC2035
vim/src/Makefile:3031:3 SC2164
vim/src/Makefile:3033:2 SC2164
vim/src/Makefile:3034:2 SC2164
vim/src/Makefile:3061:2 SC2164
vim/src/Makefile:3064:2 SC2164
vim/src/Makefile:3065:2 SC2164
vim/src/Makefile:3067:2 SC2164
vim/src/Makefile:3068:2 SC2164
vim/src/Makefile:3074:2 SC2164
vim/src/Makefile:3076:2 SC2164
vim/src/Makefile:3078:2 SC2164
vim/src/Makefile:3136:9 SC2006
vim/src/Makefile:3136:9 SC2046
vim/src/Makefile:3136:53 SL3002
vim/src/Makefile:3136:68 SC2006
vim/src/Makefile:3136:68 SC2046
vim/src/Makefile:3137:13 SC2086
vim/src/Makefile:3150:38 SC2086
vim/src/Makefile:3151:17 SC2086
vim/src/Makefile:3153:33 SC2086
vim/src/Makefile:3154:17 SC2086
vim/src/Makefile:3156:34 SC2086
vim/src/Makefile:3157:17 SC2086
vim/src/Makefile:3160:8 SC2006
vim/src/Makefile:3160:18 SC2086
vim/src/Makefile:3167:9 SC2035
vim/src/Makefile:3869:6 SC2086
vim/src/Makefile:3870:22 SC2086
vim/src/Makefile:3872:4 SC2254
vim/src/Makefile:3873:33 SC2086
vim/src/Makefile:3878:4 SC2254
vim/src/Makefile:3878:81 SC2086
vim/src/Makefile:3879:4 SC2254
vim/src/Makefile:3879:81 SC2086
vim/src/Makefile:3880:4 SC2254
vim/src/Makefile:3880:81 SC2086
vim/src/Makefile:3881:4 SC2254
vim/src/Makefile:3881:84 SC2086
vim/src/Makefile:3882:50 SC2086
vim/src/Makefile:3884:19 SC2086
vim/src/Makefile:3885:48 SC2086
wireguard-make/src/Makefile:91:11 SC2035
wireguard-make/src/Makefile:91:15 SC2035
wireguard-make/src/Makefile:102:31 SC2166
wireguard-make/src/Makefile:104:31 SC2166
