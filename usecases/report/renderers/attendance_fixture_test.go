package report_renderers

const baseOverviewJSON = `{
  "kpis": {
    "engaged": 142, "finished": 90, "ongoing": 30, "pending": 22,
    "new_leads": 40, "unassigned_backlog": 5,
    "avg_handle_mins": 12.345, "avg_wait_mins": 3.2, "avg_frt_mins": 1.5,
    "avg_rating": null, "csat_available": false,
    "frt_sla_percent": null, "resolution_sla_percent": null, "sla_available": false
  },
  "hourly": [{"hour": 0, "count": 3}, {"hour": 9, "count": 21}],
  "status_distribution": {"finished": 90, "ongoing": 30, "pending": 22, "total": 142},
  "by_department": [],
  "by_member": [],
  "frt": {"avg_mins": null, "median_mins": null, "human_avg_mins": null, "ai_avg_mins": null,
          "sample_count": 0, "human_samples": 0, "ai_samples": 0, "available": false},
  "ai": {"sessions": 0, "contained": 0, "handed_off": 0, "abandoned": 0, "open_sessions": 0,
         "containment_rate": 0, "handoff_rate": 0, "avg_ai_messages": 0, "available": false},
  "queue": {"enqueued": 0, "connected": 0, "abandoned": 0, "overflow": 0, "queue_full": 0,
            "cancelled": 0, "avg_asa_mins": null, "abandon_rate": 0, "available": false},
  "occupancy": {"avg_occupancy_pct": null, "agents_sampled": 0, "online_ms": 0, "on_call_ms": 0,
                "team_occupancy_pct": null, "team_idle_pct": null, "available": false},
  "live": {"online": 0, "in_call": 0, "free": 0, "idle_rate_pct": null,
           "busy_rate_pct": null, "has_data": false},
  "channel_mix": [],
  "messaging": {"avg_messages_per_conversation": null, "avg_inbound": null, "avg_outbound": null,
                "conversations_with_messages": 0, "available": false},
  "reopen": {"reopened_count": 0, "finished_event_count": 0, "reopen_rate": null, "available": false},
  "finished_by_source": {"human": 0, "ai": 0, "system": 0, "total": 0, "available": false},
  "stages": {"funnels": [], "staged_engaged": 0, "staged_shell": 0,
             "unstaged_engaged": 0, "unstaged_shell": 0, "stuck": 0, "available": false},
  "period": {"timezone": "", "open_days_total": 0, "open_days_done": 0, "open_days_left": 0,
             "open_minutes": 0, "elapsed_pct": 0, "available": false},
  "projections": [],
  "standing": {"targets_set": 0, "on_track": 0, "at_risk": 0, "off_track": 0,
               "on_track_pct": 0, "available": false},
  "trend": {"series": [], "unbucketed": 0, "available": false},
  "revenue": {"currencies": [], "by_owner": [], "unattributed": 0, "unowned_count": 0,
              "mixed_currencies": false, "available": false},
  "backlog_xray": {"total": 0, "available": false},
  "quality": {"threshold": 0, "rows": [], "adjacent": [], "enabled_at": null,
              "not_captured": 0, "available": false},
  "team_ranking": {"rank_metric_key": "", "min_sample": 0, "team_average": 0,
                   "members": [], "adjacent": [], "available": false},
  "rework": {"rows": [], "adjacent": [], "cost_available": false, "available": false},
  "generated_at": "2026-08-17T12:00:00Z",
  "definitions": {"period_scope": "", "status_mapping": "", "wait_time": "", "handle_time": "",
                  "resolution": "", "csat": "", "sla": ""}
}`

const stageRowJSON = `{
  "stage_id": "s1", "stage_name": "Inscrição", "color": "#00D09A", "position": 1,
  "is_won": false, "is_lost": false,
  "engaged": 600, "shell": 40, "total": 640,
  "finished": 100, "ongoing": 400, "pending": 100,
  "pct_of_funnel": 75, "pct_of_staged": 60,
  "avg_days_in_stage": 3.2, "oldest_days_in_stage": 41,
  "stuck": 87, "stuck_after_days": 12, "rot_days_set": true
}`

const stageBlockJSON = `{
  "funnels": [
    {
      "funnel_id": "f1", "funnel_name": "FUNIL UNIFECAF", "is_default": true,
      "engaged": 800, "shell": 40, "total": 840, "stuck": 87, "pct_of_staged": 80,
      "stages": [__STAGE__]
    },
    {
      "funnel_id": "f2", "funnel_name": "NÃO USAR", "is_default": false,
      "engaged": 200, "shell": 0, "total": 200, "stuck": 0, "pct_of_staged": 20,
      "stages": [{"stage_id": "s2", "stage_name": "Inscrição", "color": "#00D09A",
                  "position": 1, "is_won": false, "is_lost": false,
                  "engaged": 200, "shell": 0, "total": 200,
                  "finished": 100, "ongoing": 400, "pending": 100,
                  "pct_of_funnel": 75, "pct_of_staged": 60,
                  "avg_days_in_stage": 3.2, "oldest_days_in_stage": 41,
                  "stuck": 87, "stuck_after_days": 12, "rot_days_set": true}]
    }
  ],
  "staged_engaged": 1000, "staged_shell": 40,
  "unstaged_engaged": 120, "unstaged_shell": 10,
  "stuck": 87, "available": true
}`

const xrayDimensionJSON = `{
  "dimension": "__NAME__",
  "buckets": [
    {"key": "web", "label": "Web", "count": 1474, "pct": 23.9},
    {"key": "_other", "count": 140, "pct": 2.3}
  ],
  "measured": 6162, "unknown": 12, "available": true
}`

const executiveOverviewJSON = `{
  "kpis": {
    "engaged": 2038, "finished": 1538, "ongoing": 300, "pending": 200,
    "new_leads": 40, "unassigned_backlog": 5,
    "avg_handle_mins": null, "avg_wait_mins": null, "avg_frt_mins": null,
    "avg_rating": null, "csat_available": false,
    "frt_sla_percent": null, "resolution_sla_percent": null, "sla_available": false
  },
  "hourly": [],
  "status_distribution": {"finished": 1538, "ongoing": 300, "pending": 200, "total": 2038},
  "by_department": [],
  "by_member": [],
  "frt": {"avg_mins": null, "median_mins": null, "human_avg_mins": null, "ai_avg_mins": null,
          "sample_count": 0, "human_samples": 0, "ai_samples": 0, "available": false},
  "ai": {"sessions": 0, "contained": 0, "handed_off": 0, "abandoned": 0, "open_sessions": 0,
         "containment_rate": 0, "handoff_rate": 0, "avg_ai_messages": 0, "available": false},
  "queue": {"enqueued": 0, "connected": 0, "abandoned": 0, "overflow": 0, "queue_full": 0,
            "cancelled": 0, "avg_asa_mins": null, "abandon_rate": 0, "available": false},
  "occupancy": {"avg_occupancy_pct": null, "agents_sampled": 0, "online_ms": 0, "on_call_ms": 0,
                "team_occupancy_pct": null, "team_idle_pct": null, "available": false},
  "live": {"online": 0, "in_call": 0, "free": 0, "idle_rate_pct": null,
           "busy_rate_pct": null, "has_data": false},
  "channel_mix": [],
  "messaging": {"avg_messages_per_conversation": null, "avg_inbound": null, "avg_outbound": null,
                "conversations_with_messages": 0, "available": false},
  "reopen": {"reopened_count": 0, "finished_event_count": 0, "reopen_rate": null, "available": false},
  "finished_by_source": {"human": 0, "ai": 0, "system": 0, "total": 0, "available": false},
  "stages": {"funnels": [], "staged_engaged": 0, "staged_shell": 0,
             "unstaged_engaged": 0, "unstaged_shell": 0, "stuck": 0, "available": false},
  "period": {
    "start": "2026-09-01T00:00:00Z", "end": "2026-10-01T00:00:00Z",
    "timezone": "America/Sao_Paulo",
    "open_days_total": 22, "open_days_done": 19, "open_days_left": 3,
    "open_minutes": 11880, "elapsed_pct": 86.36, "available": true
  },
  "projections": [{
    "metric_key": "finished", "kind": "count", "direction": "higher", "cumulative": true,
    "actual": 1538, "per_open_day": 80.95, "projected": 1781, "target": 1786,
    "attain_pct": 99.72, "verdict": "at_risk", "available": true
  }],
  "standing": {"targets_set": 1, "on_track": 0, "at_risk": 1, "off_track": 0,
               "on_track_pct": 0, "cluster": "critical", "available": true},
  "trend": {
    "series": [{
      "metric_key": "finished", "kind": "count", "direction": "higher",
      "points": [
        {"bucket": "2026-07", "value": 1729, "partial": false, "projected": false},
        {"bucket": "2026-08", "value": 1538, "partial": true, "projected": false},
        {"bucket": "2026-08", "value": 1699, "partial": false, "projected": true}
      ],
      "best_bucket": "2026-07", "best_value": 1729,
      "window_from": "2026-07", "window_to": "2026-08",
      "prev_closed": 1729, "delta_pct": -1.73, "available": true
    }],
    "unbucketed": 4, "available": true
  },
  "revenue": {
    "currencies": [{
      "currency": "BRL", "value_cents": 12473295, "won_count": 119,
      "avg_ticket_cents": 104817.6, "per_open_day_cents": 656489,
      "projected_cents": 13786273, "prev_closed_cents": 13072311, "delta_pct": 5.46
    }],
    "by_owner": [], "unattributed": 2, "unowned_count": 1,
    "mixed_currencies": false, "available": true
  },
  "backlog_xray": {
    "total": 6162,
    "origin": __XRAY_origin__,
    "assignee": __XRAY_assignee__,
    "age": __XRAY_age__,
    "tenure": __XRAY_tenure__,
    "returning": __XRAY_returning__,
    "record_completeness": {
      "fields": [{"key": "name", "filled": 80, "pct": 80}],
      "measured": 100, "fully_filled": 20, "avg_fill_pct": 50, "available": true
    },
    "reachability": [
      {"channel": "whatsapp", "measured": 200, "window_open": 60, "window_closed": 140,
       "closed_pct": 70, "available": true},
      {"channel": "telegram", "measured": 12, "window_open": 0, "window_closed": 0,
       "closed_pct": 0, "available": false, "reason": "no_window_model"}
    ],
    "available": true
  },
  "quality": {
    "threshold": 30,
    "rows": [{"actor_id": "u1", "actor_kind": "human", "display_name": "Bella",
              "closes": 364, "captured": 364, "durable": 220,
              "durable_pct": 60.4, "verdict": "on_track"}],
    "adjacent": [{"actor_id": "sys", "actor_kind": "system", "display_name": "Sistema",
                  "closes": 1280, "captured": 0, "durable": 0,
                  "durable_pct": null, "verdict": ""}],
    "team": {"actor_id": "", "actor_kind": "human", "display_name": "",
             "closes": 364, "captured": 364, "durable": 220,
             "durable_pct": 60.4, "verdict": "on_track"},
    "enabled_at": "2026-09-01T00:00:00Z", "not_captured": 12, "available": true
  },
  "team_ranking": {
    "rank_metric_key": "resolved", "min_sample": 20, "team_average": 320,
    "members": [{
      "actor_id": "u1", "actor_kind": "human", "display_name": "Bella",
      "presence": "online", "avg_response_mins": 2.1, "rating": null,
      "resolution_pct": 88, "open": 10, "pending": 5, "resolved": 364,
      "rank_metric_value": 364, "per_open_day": 19.2, "per_online_hour": 2.74,
      "online_ms": 478000000, "pct_of_team_avg": 200,
      "revenue_cents": 2301531, "currency": "BRL", "avg_ticket_cents": 3175,
      "won_count": 725, "class": "elite"
    }],
    "adjacent": [],
    "totals": {"members": 1, "open": 10, "pending": 5, "resolved": 364,
               "rank_metric_value": 364, "per_open_day": 19.2, "per_online_hour": 2.74,
               "online_ms": 478000000, "revenue_cents": 2301531, "currency": "BRL",
               "avg_ticket_cents": 3175, "won_count": 725},
    "adjacent_totals": {"members": 0, "open": 0, "pending": 0, "resolved": 0,
                        "rank_metric_value": 0, "per_open_day": null, "per_online_hour": null,
                        "online_ms": 0, "revenue_cents": null, "avg_ticket_cents": null,
                        "won_count": 0},
    "available": true
  },
  "rework": {
    "rows": [{"actor_id": "u1", "actor_kind": "human", "display_name": "Bella",
              "finished": 364, "reopened": 40, "reopen_rate": 10.99,
              "templates": 12, "cost_micros": 900000}],
    "adjacent": [],
    "team": {"actor_id": "", "actor_kind": "human", "display_name": "",
             "finished": 364, "reopened": 40, "reopen_rate": 10.99,
             "templates": 12, "cost_micros": 900000},
    "unassigned": {"actor_id": "", "actor_kind": "human", "display_name": "",
                   "finished": 0, "reopened": 0, "reopen_rate": null,
                   "templates": 0, "cost_micros": 0},
    "currency": "BRL", "cost_available": true, "available": true
  },
  "generated_at": "2026-09-28T15:36:00Z",
  "definitions": {"period_scope": "", "status_mapping": "", "wait_time": "", "handle_time": "",
                  "resolution": "", "csat": "", "sla": ""}
}`
