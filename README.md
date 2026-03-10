tiered cache


implementation and robust error handling,
Multi-Tier Data tiering (L0 Otter → L1 Badger SSD → L2 Cold Tier) with 
Background Sink: Parallel L2s (Kafka + MinIO + Postgres + Any Future Backend) 
(Otter L0 + 32-Sharded Badger L1, 10 TB, max 32 KB / 4 KB-weighted payloads, slow SSD, March 2026)
with Full “Replay” / Recovery Process on Application Restart
and high consistency and lock-free where possible