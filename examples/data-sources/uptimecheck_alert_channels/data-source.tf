# Channels set up in the app (SMS, Telegram, push, Add to Slack) are read here, not managed.
data "uptimecheck_alert_channels" "all" {}

locals {
  sms_channel_ids = [for c in data.uptimecheck_alert_channels.all.items : c.id if c.type == "sms"]
}
