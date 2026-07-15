UPDATE telemetry_minute
SET
    energy_today_wh = (
        (((CAST(energy_today_wh / 10 AS INTEGER) & 65535) << 16) |
         ((CAST(energy_today_wh / 10 AS INTEGER) >> 16) & 65535)) * 10
    ),
    energy_lifetime_wh = (
        (((CAST(energy_lifetime_wh / 100 AS INTEGER) & 65535) << 16) |
         ((CAST(energy_lifetime_wh / 100 AS INTEGER) >> 16) & 65535)) * 100
    );
