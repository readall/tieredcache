tiered cache


implementation and robust error handling,
Multi-Tier Data tiering (L0 Otter → L1 Badger SSD → L2 Cold Tier(s) ) with 
Background Sink: Parallel L2s (Kafka + MinIO + Postgres + Any Future Backend) 
(Otter L0 + 32-Sharded Badger L1, 10 TB, max 32 KB / 4 KB-weighted payloads, slow SSD, March 2026)
with Full “Replay” / Recovery Process on Application Restart
and high consistency and lock-free where possible

The primary use cases could be:
1. If your service has Read:Write ratio skewed heavily towards reads
2. If the application/service needs extremely high transaction throughput with persistence and miniscule budget for failures. In such situations, network hop even withing same availability zone is not preferred
3. High amount of in-memory application data with hot/vip customer problem

This library is aimed for >90% of L0 cache hit followed by >99% of L1 cache hit.
The L0 response times should be in in range of 10us and L1 response should be sub mili-second.
On fast SSD with great RAID, the L1 can also support 20-30us response time for read.

L3 is a tier most for offline use and reduction is cost for operations.
So if there is a pattern where offline processing requires all transactions to be reliably persisted at high volume this becomes the gateway.

As write through processing is implemented, all CREATE an UPDATE operations are persisted. Reads completely lock free and thus such a high throughput.
