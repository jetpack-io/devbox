---
tile: Telemetry
displayed_sidebar: tutorialSidebar
---

import { customFields } from '@site/src/customFields';

# Telemetry

## How Devbox uses telemetry

Devbox collects some anonymized telemetry in order to detect issues, and improve the product over time. We use this data to detect bugs, understand which languages and operating systems to prioritize, and to prioritize new features.

Our team takes privacy seriously and value it ourselves, so we use the following rules and guidelines for collecting information:

1. We only collect anonymized data – nothing that is personally identifiable.
2. Data is only stored securely in SOC-2 compliant systems, and our company ({customFields.companyName}) + infrastructure is SOC-2 compliant.
3. Our users always have the ability to opt-out.
4. Our telemetry code is public and open source. You can review our implementation **[here](https://github.com/jetpack-io/devbox/blob/650e8feb1e76386594bcb2443b3fbc8c07943281/boxcli/midcobra/telemetry.go)**

## What is tracked?

The Devbox CLI captures the following information in it's telemetry:

-   CLI Version
-   Command run and arguments
-   Anonymized Device ID
-   Your OS name and Version

We do not tie this data to individual users or specific identities.

## Opting out of telemetry

For everyone who is willing to leave telemetry enabled on the Devbox CLI, we thank you for helping us improve Devbox and better understanding the user experience!

If you would like to disable Telemetry, Devbox implements **[Console Do Not Track](https://consoledonottrack.com/)**. You can disable telemetry by setting `DO_NOT_TRACK=1` in your environment variables.
