package main

func coreNotes() []noteDef {
	return []noteDef{
		{
			Title:   "ECoG Signal Preprocessing Pipeline",
			Project: 0,
			Tags:    []string{"signal-processing", "ecog", "pipeline"},
			Body: `## Overview

The preprocessing pipeline handles raw ECoG signals from the 128-channel electrode array before they reach the decoder. Each step must complete within the 10ms real-time budget.

## Pipeline Stages

1. **Bandpass filter** (0.5-300 Hz) removes DC drift and high-frequency noise
2. **Common average reference** (CAR) subtracts the mean across channels to reduce volume conduction
3. **Notch filter** at 60 Hz eliminates power line interference
4. **Feature extraction** computes log-band power in five frequency bands:
   - Theta (4-8 Hz)
   - Alpha (8-13 Hz)
   - Beta (13-30 Hz)
   - Low gamma (30-70 Hz)
   - High gamma (70-150 Hz)

High gamma is the most informative band for motor decoding. See [[Decoder Calibration Protocol]] for how these features feed into the Kalman filter.

## Latency Budget

| Stage | Target | Measured |
|---|---|---|
| Bandpass | 1ms | 0.8ms |
| CAR | 0.5ms | 0.3ms |
| Notch | 0.5ms | 0.4ms |
| Feature extraction | 3ms | 2.7ms |
| **Total** | **5ms** | **4.2ms** |

Leaves 5.8ms headroom for the decoder itself. #real-time #latency
`,
		},
		{
			Title:   "Decoder Calibration Protocol",
			Project: 0,
			Tags:    []string{"decoder", "calibration", "kalman-filter"},
			Body: `## Purpose

The Kalman filter decoder must be recalibrated at the start of each session because electrode impedances shift overnight. This document describes the standard calibration protocol used across all participants.

## Protocol

1. **Open-loop block** (3 min): participant watches cursor move to targets, imagines corresponding movement. Collects initial training data.
2. **Closed-loop recalibration** (2 min): decoder runs with parameters from the open-loop block. Neural data from successful trials is used to retrain.
3. **Steady-state decoding**: final parameters locked in, session begins.

Retrospective target-assisted recalibration (RTAR) is applied between blocks 1 and 2 to bootstrap the decoder from passive observation data.

## Parameters

- State vector: 2D position + 2D velocity (4 dimensions)
- Observation model: linear mapping from 640 features (128 channels x 5 bands) to state
- Process noise Q: tuned per participant, typically 1e-3 * I
- Observation noise R: estimated from open-loop residuals

Related: [[ECoG Signal Preprocessing Pipeline]], [[Participant P07 Session Notes]]

#neural-decoding #closed-loop
`,
		},
		{
			Title:   "Participant P07 Session Notes",
			Project: 0,
			Tags:    []string{"participant", "session-log"},
			Body: `## Session 2026-03-04

Participant P07 (left-hemisphere implant, 64-channel array over hand knob).

### Calibration

- Open-loop block: 47/48 targets observed (1 missed due to eye blink)
- RTAR converged in 12 iterations (typical)
- Closed-loop recalibration: 38/40 targets acquired (95%)

### Performance

- 2D cursor control: 92% target acquisition rate (8-target center-out)
- Mean acquisition time: 1.8s (previous session: 2.1s)
- Bit rate: 1.4 bits/s

### Notes

Electrode channels 42 and 87 showed elevated impedance (>1 MOhm). Excluded from decoder. Will monitor in next session -- if impedance stays high, update the channel map permanently.

P07 reported mild fatigue after 45 minutes. Shortened session to 50 min total. See [[Decoder Calibration Protocol]] for the standard protocol.

#participant-p07
`,
		},
		{
			Title:   "Wireless Transmitter Power Budget",
			Project: 0,
			Tags:    []string{"hardware", "wireless", "power"},
			Body: `## Problem

The implanted wireless transmitter must operate within a strict thermal envelope: no more than 1 degree C temperature rise at the cortical surface. Current power consumption is 18mW, which leaves only 2mW of margin.

## Breakdown

| Component | Power (mW) |
|---|---|
| ADC (128ch, 2kHz) | 4.2 |
| DSP (on-chip filtering) | 3.1 |
| RF transmitter (UWB, 20Mbps) | 8.5 |
| Voltage regulators | 2.2 |
| **Total** | **18.0** |

## Optimization Options

1. **Reduce sampling rate** to 1kHz -- saves ~1.5mW on ADC but loses high-gamma resolution above 500Hz
2. **On-chip feature extraction** -- transmit features instead of raw data, cuts RF power by ~60% but adds DSP power. Net savings estimated at 3mW.
3. **Duty-cycle RF** -- buffer 100ms of data, transmit in bursts. Adds 100ms latency which violates real-time constraints for cursor control.

Option 2 is the most promising. Requires ASIC redesign. See [[ECoG Signal Preprocessing Pipeline]] for which features to compute on-chip.

#power-budget #thermal
`,
		},
		{
			Title:   "SDK Architecture Overview",
			Project: 1,
			Tags:    []string{"architecture", "sdk", "api-design"},
			Body: `## Design Goals

The NeuroLink SDK abstracts away device-specific details so third-party developers can build BCI applications without needing neuroscience expertise.

## Layer Architecture

    Application Layer     (developer code)
        |
    SDK Public API        (NeuroLink.Connect, stream.OnDecode, etc.)
        |
    Device Abstraction    (drivers for Cortical Decoder, EEG headsets, etc.)
        |
    Transport Layer       (USB, Bluetooth, WiFi -- device-dependent)

## Key Interfaces

- **DeviceManager** -- enumerate, connect, disconnect devices
- **SignalStream** -- real-time neural data with backpressure
- **DecoderOutput** -- decoded intentions (cursor position, discrete selection, text)
- **Impedance** -- electrode health monitoring

All streaming uses a push model with configurable buffer depth. Default buffer is 500ms to absorb jitter without adding perceptible latency.

See [[REST API Specification]] for the companion cloud API, and [[Sensory Feedback Integration API]] for the haptic feedback extension.

#architecture #developer-experience
`,
		},
		{
			Title:   "REST API Specification",
			Project: 1,
			Tags:    []string{"api", "rest", "specification"},
			Body: `## Base URL

All endpoints are versioned under /api/v1/.

## Authentication

OAuth 2.0 with device authorization grant (RFC 8628) for headless BCI devices. Access tokens are JWTs with 15-minute expiry.

## Endpoints

### Devices

| Method | Path | Description |
|---|---|---|
| GET | /devices | List paired devices |
| POST | /devices/{id}/connect | Initiate connection |
| DELETE | /devices/{id}/connect | Disconnect |
| GET | /devices/{id}/impedance | Channel impedance snapshot |

### Sessions

| Method | Path | Description |
|---|---|---|
| POST | /sessions | Start a recording/decoding session |
| GET | /sessions/{id} | Session metadata |
| DELETE | /sessions/{id} | End session |
| GET | /sessions/{id}/metrics | Real-time performance metrics |

### Data

| Method | Path | Description |
|---|---|---|
| GET | /sessions/{id}/stream | WebSocket upgrade for real-time decoded output |
| GET | /sessions/{id}/raw | WebSocket upgrade for raw signal data (requires elevated permissions) |

Rate limits: 100 requests/min for REST, no limit on WebSocket frames. See [[SDK Architecture Overview]] for how the REST API fits into the overall SDK.

#api #specification
`,
		},
		{
			Title:   "Beta Program Rollout Plan",
			Project: 1,
			Tags:    []string{"launch", "beta", "commercial"},
			Body: `## Timeline

- **Week 1-2**: Internal dogfooding with Cortical Decoder team
- **Week 3-4**: Closed beta with 5 partner institutions (Stanford, APL, Battelle, BrainGate, Synchron)
- **Week 5-8**: Open beta for registered developers (cap at 50)
- **Week 9+**: GA release

## Success Criteria for GA

1. SDK latency overhead < 2ms (measured as time from device driver callback to application callback)
2. Zero data-loss events across all beta sessions
3. Documentation coverage > 90% of public API surface
4. At least 3 third-party demo applications published

## Pricing Model

- **Research tier**: free, rate-limited to 1 device, no raw data access
- **Professional tier**: $299/mo, up to 5 devices, raw data, priority support
- **Enterprise tier**: custom pricing, on-premise deployment option, SLA

The research tier is critical for adoption. Every BCI lab should be able to prototype on NeuroLink without budget friction.

See [[SDK Architecture Overview]] for technical details.

#business #go-to-market
`,
		},
		{
			Title:   "Micro-stimulation Parameter Space",
			Project: 2,
			Tags:    []string{"stimulation", "parameters", "somatosensory"},
			Body: `## Background

Intracortical micro-stimulation (ICMS) of somatosensory cortex (S1) can evoke tactile percepts in the contralateral hand. The challenge is mapping stimulation parameters to natural-feeling sensations.

## Parameter Dimensions

1. **Amplitude**: 10-100 uA (charge-balanced biphasic pulses)
2. **Frequency**: 50-300 Hz (perceived intensity scales with frequency up to ~200Hz, then saturates)
3. **Pulse width**: 100-400 us per phase
4. **Train duration**: continuous vs. patterned (e.g., 200ms on / 100ms off for texture)
5. **Electrode selection**: which of 32 S1 electrodes to stimulate (determines perceived location on hand)

## Key Findings from P03

- Electrodes 4, 7, 12 reliably evoke fingertip sensations (thumb, index, middle)
- Amplitude below 20uA: no percept. Above 80uA: uncomfortable tingling.
- Sweet spot: 40-60uA at 100Hz produces "gentle pressure" percept
- Patterned stimulation (100ms on/50ms off at 200Hz) produces distinct "texture" percept vs. continuous

See [[Closed-Loop Grasp Controller]] for how these percepts are used during object manipulation.

#percept-mapping #icms
`,
		},
		{
			Title:   "Closed-Loop Grasp Controller",
			Project: 2,
			Tags:    []string{"closed-loop", "grasp", "control"},
			Body: `## Architecture

The grasp controller combines decoded motor intention (from [[Decoder Calibration Protocol]]) with sensory feedback to enable stable object grasping.

## Control Loop

1. **Motor decode**: Kalman filter outputs desired grip force (continuous, 0-10N range)
2. **Prosthetic actuation**: motor command sent to robotic hand via CAN bus
3. **Force sensing**: strain gauges on each fingertip report actual grip force
4. **Sensory encoding**: force magnitude mapped to ICMS amplitude (linear, 0N=0uA, 10N=80uA)
5. **Stimulation delivery**: biphasic pulses at 100Hz to corresponding S1 electrode
6. **Loop time**: 20ms total (50Hz update rate)

## Results (Preliminary)

Without feedback: participants crushed soft objects 34% of the time (excessive grip force).

With feedback: crush rate dropped to 8%. Participants reported "feeling" the object and naturally modulating grip. P03 described it as "like wearing thick gloves -- not natural, but useful."

## Open Questions

- Can we reduce loop time to 10ms for finer control?
- Patterned stimulation for slip detection (see [[Micro-stimulation Parameter Space]])
- Long-term stability of percept maps over weeks/months

#closed-loop #prosthetics
`,
		},
		{
			Title:   "Sensory Feedback Integration API",
			Project: 2,
			Tags:    []string{"api", "integration", "sdk"},
			Body: `## Motivation

The [[SDK Architecture Overview]] currently only supports decoding (brain to machine). This spec extends it with an encoding path (machine to brain) for sensory feedback.

## Proposed API

    interface FeedbackChannel {
      electrodeId: number;
      percept: PerceptType;      // "pressure" | "vibration" | "texture"
      location: HandRegion;       // "thumb_tip" | "index_tip" | etc.
    }

    interface StimulationCommand {
      channel: FeedbackChannel;
      amplitude: number;          // 0.0 - 1.0 (normalized, mapped to safe uA range per channel)
      pattern: StimPattern;       // "continuous" | "pulsed" | custom waveform
      duration: number;           // milliseconds, 0 = until cancelled
    }

    // Usage
    const thumb = sdk.feedback.getChannel("thumb_tip");
    sdk.feedback.stimulate({
      channel: thumb,
      amplitude: 0.5,
      pattern: "continuous",
      duration: 0,
    });

## Safety Constraints

- SDK enforces per-channel charge limits (no application can exceed safe thresholds)
- Amplitude is normalized 0-1; actual uA mapping is per-participant calibration
- Maximum stimulation duration without pause: 30 seconds (auto-ramp-down)
- All commands logged for clinical audit trail

See [[Micro-stimulation Parameter Space]] for the underlying parameter ranges and [[Beta Program Rollout Plan]] for timeline.

#api #sensory-feedback #safety
`,
		},
	}
}
