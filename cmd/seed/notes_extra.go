package main

var extraNotes = []noteDef{
	{
		Title:   "Motor Cortex Channel Selection Strategy",
		Project: 0,
		Tags:    []string{"signal-processing", "motor-cortex", "feature-selection"},
		Body: `## Channel Selection for High-Dimensional ECoG Arrays

Our 256-channel ECoG grid over primary motor cortex (M1) and premotor cortex (PMd) yields far more data than the decoder can process in real time. We need a principled channel selection strategy that balances decoding accuracy against computational cost.

### Current Approach

We use a two-stage pipeline described in [[ECoG Signal Preprocessing Pipeline]]:

1. **Variance-based pruning** -- discard channels with signal variance below 2x the noise floor estimate (typically removes 30-50 channels with poor contact impedance).
2. **Mutual information ranking** -- compute MI between each channel's high-gamma band (70-150 Hz) and the target kinematic variable (2D cursor velocity). Retain the top N channels.

### Results from Participant P07

Per [[Participant P07 Session Notes]], channel counts of 64 and 96 yield comparable offline accuracy (R^2 = 0.71 vs 0.74 for 2D velocity). However, the 64-channel subset runs comfortably within our 10 ms decode window on the embedded processor specified in [[Wireless Transmitter Power Budget]].

| Channel Count | Offline R^2 | Decode Latency (ms) | Power Draw (mW) |
|---------------|-------------|----------------------|------------------|
| 256 (all)     | 0.76        | 38.2                 | 410              |
| 96            | 0.74        | 14.1                 | 185              |
| 64            | 0.71        | 8.7                  | 130              |
| 32            | 0.59        | 4.3                  | 72               |

### Planned Improvements

We are evaluating the [[Transformer Decoder Architecture]] from the Neural Signal AI team, which uses attention-based channel weighting that may eliminate the need for explicit channel selection. Initial offline benchmarks look promising. See also [[Spike Sorting Algorithm Comparison]] for complementary approaches on intracortical arrays.

### Open Questions

- Should we retrain channel selection per session, or can we fix a stable subset across sessions during [[Decoder Calibration Protocol]]?
- How does channel dropout due to electrode degradation (tracked by [[Electrode Impedance QC Protocol]]) affect long-term stability?

#channel-selection #real-time-decoding
`,
	},
	{
		Title:   "Kalman Filter State Estimation Tuning",
		Project: 0,
		Tags:    []string{"decoding", "kalman-filter", "state-estimation"},
		Body: `## Kalman Filter Parameter Tuning for Cursor Control

The Cortical Decoder uses a steady-state Kalman filter (KF) to map neural features to 2D cursor kinematics. This note documents our tuning procedure and current parameter values.

### State-Space Model

    x(t) = A * x(t-1) + w(t)     // state transition
    z(t) = H * x(t) + v(t)       // observation model

Where x(t) = [pos_x, pos_y, vel_x, vel_y]^T and z(t) is the vector of neural features from the selected channels (see [[Motor Cortex Channel Selection Strategy]]).

### Tuning Procedure

Following [[Decoder Calibration Protocol]], we perform a two-block calibration:

1. **Open-loop block** (2 min): Participant P07 observes a cursor moving through center-out targets. Neural data is recorded passively.
2. **Closed-loop recalibration** (3 min): Initial KF parameters are estimated from block 1. Participant actively controls the cursor while we collect paired neural-kinematic data.

The observation noise covariance R is estimated empirically from calibration data. The process noise covariance Q is scaled by a smoothing factor alpha:

    Q = alpha * I_4x4

We sweep alpha in [0.001, 0.01, 0.1] and select the value that minimizes mean-squared error on a held-out validation segment.

### Current Best Parameters (P07, Session 42)

- alpha = 0.008
- Observation model H: 64 x 4 matrix (64 channels, 4 state dims)
- Average decode latency: 8.7 ms per timestep
- Online accuracy: 87% target acquisition within 5 seconds

### Integration with AI Models

The [[Training Data Pipeline]] team has proposed replacing the linear observation model H with a nonlinear neural network mapping. We would retain the Kalman framework for temporal smoothing but use a learned observation function. This is tracked in our next milestone.

#kalman-filter #cursor-control #online-decoding
`,
	},
	{
		Title:   "Velocity vs Position Control Comparison",
		Project: 0,
		Tags:    []string{"decoding", "control-mode", "human-factors"},
		Body: `## Comparing Velocity and Position Control Paradigms

A key design decision in the [[Decoder Calibration Protocol]] is whether the decoded neural signal maps to cursor velocity or cursor position. This note summarizes our internal comparison study.

### Background

- **Position control**: decoded output directly sets cursor (x, y) coordinates. Intuitive but noisy -- small decoding errors produce jittery cursor movement.
- **Velocity control**: decoded output sets cursor velocity (dx/dt, dy/dt). Cursor drifts when the user is idle, but the Kalman filter (see [[Kalman Filter State Estimation Tuning]]) provides natural smoothing.

### Experimental Setup

We tested both modes with Participant P07 ([[Participant P07 Session Notes]]) over 8 sessions using a center-out reaching task with 8 targets.

### Results

| Metric                        | Position Control | Velocity Control |
|-------------------------------|------------------|------------------|
| Target acquisition rate       | 72%              | 89%              |
| Mean time to target (s)       | 4.8              | 3.2              |
| Path efficiency (straight=1)  | 0.41             | 0.67             |
| Subjective preference (1-10)  | 5.2              | 7.8              |
| Overshoot rate                | 38%              | 12%              |

Velocity control is clearly superior for our current decoder architecture. The [[Closed-Loop Grasp Controller]] in the Sensory Feedback Loop project uses a similar velocity-based approach for hand aperture control.

### Considerations for Clinical Deployment

For the [[Phase I Trial Design]], we will standardize on velocity control. The [[Adaptive Difficulty Algorithm]] from the Neurorehab team could be adapted to gradually increase target difficulty as participants gain proficiency.

### Next Steps

- Investigate hybrid mode where position and velocity are blended based on confidence of the decoder output
- Evaluate acceleration control for participants with strong neural modulation

#control-paradigm #velocity-control #clinical
`,
	},
	{
		Title:   "Online Adaptive Decoder Retraining",
		Project: 0,
		Tags:    []string{"adaptive-decoding", "online-learning", "stability"},
		Body: `## Adaptive Decoder Retraining During BCI Sessions

Neural signals drift over time due to electrode micro-motion, gliosis, and changes in neural tuning. This note describes our online adaptive retraining strategy that maintains decoder performance without interrupting the user.

### The Drift Problem

After initial calibration (see [[Decoder Calibration Protocol]]), decoder accuracy typically degrades by 15-25% over 60 minutes. Participant P07 data from [[Participant P07 Session Notes]] shows clear feature distribution shifts within a single session.

### Retrospective Target Inference (RTI)

We use RTI to generate pseudo-labels for unsupervised adaptation:

1. During closed-loop control, the participant acquires targets.
2. After each successful acquisition, we retrospectively label the preceding 2 seconds of neural data with the known target location.
3. These pseudo-labeled samples are used to update the Kalman filter observation model H via recursive least squares (RLS).

### Implementation

    // Pseudo-code for RTI update
    func (d *Decoder) OnTargetAcquired(targetPos Vec2, trialData []NeuralFrame) {
        labels := inferKinematicsFromTarget(targetPos, trialData)
        for i, frame := range trialData {
            d.rlsUpdate(frame.Features, labels[i])
        }
    }

The RLS forgetting factor lambda controls the adaptation rate. We use lambda = 0.998, which gives an effective memory of ~500 samples (about 25 seconds at 20 Hz decode rate).

### Safeguards

- If the Mahalanobis distance of incoming features exceeds 4 sigma from the training distribution, we freeze adaptation to prevent catastrophic divergence.
- Maximum parameter change per update is clamped to prevent sudden jumps.
- All parameter updates are logged for post-hoc analysis via the [[Real-Time Telemetry Dashboard]].

### Results

With RTI enabled, accuracy degradation over 60 minutes drops from 22% to 4% on average. This is critical for the [[Phase I Trial Design]] where sessions may last 2+ hours.

#adaptive-decoding #online-learning #drift-compensation
`,
	},
	{
		Title:   "High Gamma Feature Extraction Methods",
		Project: 0,
		Tags:    []string{"signal-processing", "high-gamma", "feature-extraction"},
		Body: `## High Gamma Band Feature Extraction for ECoG Decoding

High gamma activity (HGA, 70-150 Hz) is the primary neural feature driving our cortical decoder. This note documents the extraction methods evaluated and currently deployed.

### Why High Gamma?

HGA in ECoG is strongly correlated with local neuronal population spiking and is the most informative frequency band for motor decoding. The [[ECoG Signal Preprocessing Pipeline]] feeds cleaned signals into the feature extraction stage described here.

### Methods Evaluated

**1. Band-pass + Analytic Amplitude**
- Butterworth band-pass filter (70-150 Hz, 4th order)
- Hilbert transform to obtain analytic amplitude
- Log transform and z-score normalization
- Bin into 50 ms windows with 10 ms step

**2. Multi-taper Spectral Estimation**
- 3 Slepian tapers, 200 ms window
- Extract power in 70-150 Hz band
- Higher spectral resolution but 5x more compute

**3. Wavelet Decomposition**
- Complex Morlet wavelets at 10 Hz spacing from 70-150 Hz
- Average across wavelet frequencies per time bin
- Good time-frequency tradeoff

### Benchmark Comparison

| Method       | Offline R^2 | Compute per Bin (us) | Latency Contribution (ms) |
|--------------|-------------|----------------------|----------------------------|
| BP+Hilbert   | 0.69        | 42                   | 1.2                        |
| Multi-taper  | 0.71        | 215                  | 5.8                        |
| Wavelet      | 0.70        | 128                  | 3.4                        |

### Decision

We deploy BP+Hilbert for real-time operation given the minimal accuracy cost and significant latency savings. This is particularly important given the power constraints in [[Wireless Transmitter Power Budget]]. For offline analyses feeding the [[Training Data Pipeline]], we use multi-taper estimation for maximum accuracy.

### Integration Note

The extracted HGA features are passed directly to [[Kalman Filter State Estimation Tuning]] or to the experimental [[Transformer Decoder Architecture]] pathway.

#high-gamma #spectral-analysis #feature-extraction
`,
	},
	{
		Title:   "Decoder Latency Budget Breakdown",
		Project: 0,
		Tags:    []string{"latency", "real-time", "systems-engineering"},
		Body: `## End-to-End Latency Budget for the Cortical Decoder

Total acceptable latency from neural event to cursor update is 50 ms for fluid cursor control. This note breaks down how we allocate that budget across the pipeline.

### Latency Components

| Stage                        | Budget (ms) | Measured (ms) | Owner                        |
|------------------------------|-------------|---------------|------------------------------|
| Signal acquisition + ADC     | 5           | 3.2           | Hardware team                |
| Wireless transmission        | 8           | 6.5           | [[Wireless Transmitter Power Budget]] |
| Preprocessing (notch, CAR)   | 5           | 3.8           | [[ECoG Signal Preprocessing Pipeline]] |
| Feature extraction (HGA)     | 5           | 1.2           | [[High Gamma Feature Extraction Methods]] |
| Decode (Kalman filter)       | 10          | 8.7           | [[Kalman Filter State Estimation Tuning]] |
| Smoothing + output           | 2           | 1.1           | Post-processing              |
| Display render               | 15          | 12.4          | Frontend                     |
| **Total**                    | **50**      | **36.9**      |                              |

### Margin Analysis

We currently have 13.1 ms of margin. This is important because:

1. **Wireless retransmissions** can add 2-8 ms in noisy RF environments (see [[RF Link Budget Analysis]] from the Implant Telemetry team).
2. **Adaptive retraining** (see [[Online Adaptive Decoder Retraining]]) adds periodic computation spikes of ~5 ms.
3. **Future decoder upgrades** to attention-based models from the [[Transformer Decoder Architecture]] team will consume more compute.

### Monitoring

Latency is tracked per-stage in real time via the [[Real-Time Telemetry Dashboard]]. Any stage exceeding its budget by >50% triggers an alert. Historical latency data is stored for post-hoc analysis.

### Worst-Case Analysis

Under worst-case conditions (RF retry + adaptive update + thermal throttling), total latency reaches ~48 ms. This is within budget but leaves no room for display jank. We are evaluating whether the [[Implant Firmware OTA Update Protocol]] can optimize the wireless stack to reclaim margin.

#latency #systems-engineering #real-time
`,
	},
	{
		Title:   "Participant P07 Long-Term Stability Report",
		Project: 0,
		Tags:    []string{"clinical", "longitudinal", "stability"},
		Body: `## Longitudinal Decoder Stability for Participant P07

This report tracks decoder performance metrics for Participant P07 over the 6-month period since implantation. Data is drawn from regular sessions documented in [[Participant P07 Session Notes]].

### Overview

P07 was implanted with a 256-channel ECoG array over left M1/PMd. Initial decoder calibration was performed at week 2 post-implant per [[Decoder Calibration Protocol]].

### Monthly Performance Summary

| Month | Sessions | Mean Accuracy (%) | Mean Bitrate (bits/min) | Active Channels |
|-------|----------|--------------------|--------------------------|-----------------|
| 1     | 12       | 82                 | 24.3                     | 241             |
| 2     | 14       | 87                 | 28.7                     | 238             |
| 3     | 11       | 89                 | 31.2                     | 234             |
| 4     | 13       | 88                 | 30.8                     | 229             |
| 5     | 10       | 86                 | 29.4                     | 221             |
| 6     | 12       | 85                 | 28.9                     | 215             |

### Key Observations

- Performance peaked at month 3, consistent with published literature on ECoG learning effects.
- Gradual channel loss (26 channels over 6 months) is due to impedance increases tracked by [[Electrode Impedance QC Protocol]]. Rate is within expected bounds.
- [[Online Adaptive Decoder Retraining]] has been critical for maintaining performance despite channel loss.
- The [[Motor Cortex Channel Selection Strategy]] is re-run monthly to account for channel dropout.

### Comparison with Trial Protocol

The [[Phase I Trial Design]] specifies a minimum performance threshold of 70% accuracy and 20 bits/min for the primary endpoint. P07 has been above threshold for all 6 months.

### Adverse Events

No serious adverse events. Two instances of transient headache reported and logged per [[Adverse Event Reporting SOP]]. No device-related complications.

### Recommendation

Continue with current protocol. Schedule electrode impedance imaging at month 9 to project long-term array viability.

#longitudinal #participant-p07 #clinical-data
`,
	},
	{
		Title:   "Imagined Movement Decoder Extension",
		Project: 0,
		Tags:    []string{"decoding", "motor-imagery", "paradigm"},
		Body: `## Extending the Decoder to Imagined Movement

Currently, the Cortical Decoder operates on attempted movement signals from participants with residual motor function. This note outlines the plan to extend support to purely imagined movement for participants with complete paralysis.

### Motivation

Several candidates for the [[Phase I Trial Design]] have complete C3-C4 spinal cord injuries with no residual motor output. For these participants, we must decode imagined (not attempted) movement, which produces weaker and more variable neural signals.

### Technical Challenges

1. **Lower signal-to-noise ratio** -- imagined movement HGA amplitudes are typically 40-60% of attempted movement levels. Our [[High Gamma Feature Extraction Methods]] may need lower thresholds.
2. **No ground-truth kinematics** -- we cannot use the RTI approach from [[Online Adaptive Decoder Retraining]] because there is no overt movement to correlate with.
3. **Calibration difficulty** -- the standard [[Decoder Calibration Protocol]] relies on observation-based calibration, which may transfer poorly to imagined movement.

### Proposed Approach

**Phase 1: Hybrid calibration**
- Use error-related potentials (ErrP) detected during cursor control as implicit feedback
- Combine with target-based pseudo-labels when target distance is unambiguous

**Phase 2: Transfer learning**
- Pre-train decoder on attempted movement data from participants like P07 ([[Participant P07 Session Notes]])
- Fine-tune on imagined movement data using the approach from [[Training Data Pipeline]]
- The [[Transformer Decoder Architecture]] may be particularly suitable due to its ability to learn cross-participant representations

### Collaboration

The [[Spinal Cord Interface]] team has relevant experience with their [[Locomotion Decoder Design]] which faces similar imagined-movement challenges for lower-limb decoding. We should coordinate approaches.

### Timeline

- Months 1-2: Offline analysis of existing imagined movement datasets
- Months 3-4: Online pilot with 2 participants
- Month 5: Integration into main decoder codebase

#motor-imagery #imagined-movement #transfer-learning
`,
	},
	{
		Title:   "Two-Dimensional vs Three-Dimensional Decoding",
		Project: 0,
		Tags:    []string{"decoding", "dimensionality", "prosthetics"},
		Body: `## Scaling the Decoder from 2D Cursor to 3D Prosthetic Control

The current decoder outputs 2D cursor velocity. To control a robotic arm or [[Closed-Loop Grasp Controller]], we need to scale to 3D position + orientation + grasp. This note analyzes the feasibility.

### Dimensionality Scaling

| DOF Configuration | State Dims | Min Channels (est.) | Feasibility |
|-------------------|-----------|----------------------|-------------|
| 2D cursor (current) | 4 (pos+vel) | 32-64 | Demonstrated |
| 3D cursor | 6 (pos+vel) | 48-96 | High confidence |
| 3D + wrist rotation | 8 | 64-128 | Medium confidence |
| 3D + wrist + grasp | 10 | 96-160 | Under investigation |
| Full arm (7 DOF + grasp) | 16 | 160-256 | Requires full array |

### Current Limitations

Our [[Kalman Filter State Estimation Tuning]] uses a linear observation model that may not capture the complex, nonlinear relationship between neural activity and high-DOF kinematics. Options:

1. **Unscented Kalman Filter (UKF)** -- handles mild nonlinearity, moderate compute increase
2. **Neural network observation model** -- most flexible, but latency implications per [[Decoder Latency Budget Breakdown]]
3. **Hierarchical decoding** -- decode endpoint + grasp type as discrete classes, then continuous trajectories within each class

### Integration with Sensory Feedback

3D control is significantly more effective with proprioceptive feedback. The [[Sensory Feedback Integration API]] from the Sensory Feedback Loop project must provide joint angle and contact force information to the participant. See also [[Micro-stimulation Parameter Space]] for the stimulation parameters that encode this feedback.

### Next Steps

- Implement 3D extension of current Kalman filter (add z-axis velocity and position states)
- Collect 3D reaching data from P07 using the center-out task with depth targets
- Benchmark against [[Transformer Decoder Architecture]] for 3D decoding

#3d-decoding #prosthetic-control #high-dof
`,
	},
	{
		Title:   "Error-Related Potential Detection Module",
		Project: 0,
		Tags:    []string{"error-detection", "ErrP", "signal-processing"},
		Body: `## Error-Related Potential Detection for Implicit Feedback

Error-related potentials (ErrPs) are neural signatures that occur when the BCI produces an incorrect output. Detecting these signals in real time provides implicit feedback to the decoder without requiring explicit user correction.

### Background

ErrPs are well-characterized in EEG literature but less studied in ECoG. Our ECoG arrays over M1 also cover regions of prefrontal cortex where ErrPs are generated. The [[ECoG Signal Preprocessing Pipeline]] already preserves the low-frequency bands (1-10 Hz) where ErrPs are most prominent.

### Detection Algorithm

    type ErrPDetector struct {
        Template    []float64  // averaged ErrP waveform
        Threshold   float64    // correlation threshold
        ChannelIdx  []int      // prefrontal channels
        WindowMs    int        // detection window (300-600 ms post-event)
    }

    func (d *ErrPDetector) Detect(epoch [][]float64) (bool, float64) {
        projected := d.spatialFilter(epoch)
        corr := pearsonCorrelation(projected, d.Template)
        return corr > d.Threshold, corr
    }

### Training the Template

We use a cursor correction task where P07 ([[Participant P07 Session Notes]]) observes the cursor making deliberate errors (50% of trials). The template is the average ERP across error trials, computed over the prefrontal channel subset.

### Applications

1. **Decoder recalibration** -- ErrP detection supplements [[Online Adaptive Decoder Retraining]] by identifying erroneous outputs even when the target is ambiguous.
2. **Undo/redo interface** -- ErrP triggers an undo of the last decoded action, improving user experience.
3. **Imagined movement training** -- critical for [[Imagined Movement Decoder Extension]] where no ground-truth kinematics are available.

### Current Performance

- Detection accuracy: 78% (true positive rate)
- False positive rate: 12%
- Detection latency: 450 ms post-error

The false positive rate is still too high for production use. We are exploring improved spatial filters and collaboration with the [[Neural Signal AI]] team on learned ErrP classifiers.

#error-potential #implicit-feedback #closed-loop
`,
	},
	{
		Title:   "Neural Feature Stability Across Sleep Cycles",
		Project: 0,
		Tags:    []string{"stability", "sleep", "chronic-recording"},
		Body: `## Impact of Sleep on Neural Feature Distributions

Chronic BCI users like P07 experience significant shifts in neural feature distributions after sleep. This note investigates the phenomenon and its impact on decoder performance.

### Observation

During [[Participant P07 Session Notes]] analysis, we noticed that the first 5-10 minutes of each morning session show significantly degraded decoder performance (accuracy drops to 60-65%) compared to end-of-previous-day performance (85-89%). The [[Online Adaptive Decoder Retraining]] module requires 5-8 minutes to compensate.

### Analysis

We extracted HGA features (see [[High Gamma Feature Extraction Methods]]) from the last 10 minutes of day N and first 10 minutes of day N+1 for 30 consecutive session pairs.

**Feature Distribution Shifts:**
- Mean HGA amplitude: shifts by 0.3-0.8 standard deviations overnight
- Channel correlation structure: average pairwise correlation changes by 15-22%
- Spatial pattern of most informative channels: 20-30% turnover in top-32 set

### Hypotheses

1. **Synaptic homeostasis** -- sleep-related synaptic downscaling alters the gain of cortical circuits
2. **Electrode micro-motion** -- positional shifts during sleep change recording geometry
3. **Cortical state differences** -- arousal level and attentional state differ at session start

### Mitigation Strategies

- **Morning warm-up protocol**: 3-minute guided calibration block before each session, integrated into [[Decoder Calibration Protocol]]
- **Overnight model blending**: maintain a rolling average of decoder parameters across the last 5 sessions, use as starting point each morning
- **State-dependent models**: train separate day/night models and blend based on detected cortical state

### Relevance to Clinical Deployment

For the [[Phase I Trial Design]], we must define whether the morning warm-up counts toward the session time endpoint. The [[IRB Protocol Amendments]] may need updating to reflect this protocol addition.

#sleep-effects #chronic-bci #feature-drift
`,
	},
	{
		Title:   "Offline Replay Analysis Framework",
		Project: 0,
		Tags:    []string{"tooling", "analysis", "replay"},
		Body: `## Offline Replay and Analysis Framework

To accelerate decoder development, we built a replay framework that re-runs the full decoding pipeline on recorded neural data. This enables rapid iteration without requiring participant sessions.

### Architecture

    SessionRecording/
        metadata.json       // session info, channel map, params
        neural_raw.bin      // raw ECoG samples (int16, 30 kHz)
        neural_features.bin // pre-extracted features (float32, 20 Hz)
        kinematics.bin      // cursor position + target state
        events.json         // trial markers, target acquisitions
        decoder_state.bin   // Kalman filter state snapshots

### Usage

    replay := NewReplaySession("sessions/P07_042/")
    replay.SetDecoder(newDecoder)         // swap in experimental decoder
    replay.SetFeatureExtractor(newHGA)    // swap feature extraction
    results := replay.Run()
    fmt.Printf("Offline R^2: %.3f\n", results.Rsquared)

### Integration Points

- Recordings are generated automatically during live sessions managed by the [[Decoder Calibration Protocol]]
- Feature files are compatible with the [[Training Data Pipeline]] for model training
- Results are uploaded to the [[Real-Time Telemetry Dashboard]] for comparison with online metrics

### Data Management

Each session recording is approximately 2.1 GB (256 channels, 30 kHz, 60-minute session). Storage and transfer are handled by the [[Multi-Site Data Sync Architecture]] on the BCI Cloud Platform. All recordings are processed through the [[Neural Data Anonymization]] pipeline before being shared across sites.

### Recent Improvements

- Added support for replaying adaptive decoder updates ([[Online Adaptive Decoder Retraining]]) with configurable forgetting factors
- Parallel replay across multiple sessions for batch parameter sweeps
- Integration with the [[Spike Sorting Algorithm Comparison]] for intracortical data replay

### Known Limitations

- Replay cannot capture closed-loop effects (participant would have behaved differently with a different decoder)
- Display latency component from [[Decoder Latency Budget Breakdown]] is not modeled

#replay #offline-analysis #tooling
`,
	},
	{
		Title:   "Decoder Output Confidence Estimation",
		Project: 0,
		Tags:    []string{"confidence", "decoding", "safety"},
		Body: `## Real-Time Confidence Estimation for Decoder Outputs

For safety-critical applications like prosthetic arm control, the decoder must not only produce kinematic estimates but also indicate its confidence. Low-confidence outputs should be suppressed or attenuated.

### Approach

The Kalman filter in [[Kalman Filter State Estimation Tuning]] naturally produces a state covariance matrix P(t) that represents uncertainty in the decoded state. We derive a scalar confidence score:

    confidence = 1.0 / (1.0 + trace(P_vel))

Where P_vel is the 2x2 velocity subblock of P(t). This is normalized to [0, 1] using session-specific calibration bounds.

### Confidence-Gated Control

    if confidence < threshold_low {
        // Suppress output entirely -- hold last position
        output = lastOutput
    } else if confidence < threshold_mid {
        // Attenuate -- blend with zero velocity
        alpha := (confidence - threshold_low) / (threshold_mid - threshold_low)
        output = alpha * decoded + (1 - alpha) * Vec2{0, 0}
    } else {
        output = decoded
    }

Current thresholds: threshold_low = 0.2, threshold_mid = 0.5.

### Safety Integration

Confidence scores are critical for the [[Closed-Loop Grasp Controller]] in the Sensory Feedback Loop project. A prosthetic hand must not close with full force during a low-confidence decode. The [[Sensory Feedback Integration API]] receives confidence alongside kinematic commands.

### Monitoring and Logging

Per-timestep confidence is logged via the [[Real-Time Telemetry Dashboard]]. We track:
- Mean confidence per session (target: > 0.65)
- Fraction of time in suppressed state (target: < 10%)
- Correlation between confidence and actual decoding error

### Regulatory Considerations

The [[FDA 510k Submission Timeline]] requires documentation of all safety mechanisms including confidence-gated control. We need to validate the sensitivity and specificity of the confidence threshold against clinically meaningful error categories.

#confidence-estimation #safety #regulatory
`,
	},
	{
		Title:   "Multi-Participant Decoder Generalization",
		Project: 0,
		Tags:    []string{"generalization", "multi-participant", "transfer-learning"},
		Body: `## Cross-Participant Decoder Generalization Study

Training a decoder from scratch for each new participant requires 2-4 hours of calibration data. This note explores whether a pre-trained general decoder can reduce calibration time.

### Motivation

As we scale toward the full [[Phase I Trial Design]] cohort (N=12), per-participant calibration becomes a bottleneck. If we can pre-train a decoder on existing participants and fine-tune on minimal data from new participants, we can:

- Reduce first-session calibration from 2 hours to ~15 minutes
- Improve decoder quality for early sessions before sufficient data accumulates
- Standardize the starting point across the clinical cohort

### Approach: Shared Latent Space

1. Train a neural network encoder that maps each participant's ECoG features to a shared latent space
2. Train a shared decoder from latent space to kinematics
3. For a new participant, only the encoder needs fine-tuning

This builds on the [[Transformer Decoder Architecture]] from the Neural Signal AI team, which uses a participant-specific input embedding layer with shared transformer blocks.

### Preliminary Results (3 Participants)

| Condition                      | First-Session R^2 | After 15 min Fine-Tune |
|--------------------------------|--------------------|-----------------------|
| From scratch (baseline)        | 0.31               | 0.31                  |
| Pre-trained, no fine-tuning    | 0.42               | N/A                   |
| Pre-trained + fine-tuned       | 0.42               | 0.61                  |
| Full calibration (2 hr)        | N/A                | 0.71                  |

### Data Pipeline

Cross-participant data is aggregated through the [[Training Data Pipeline]] with anonymization handled by [[Neural Data Anonymization]]. Channel mapping normalization is essential since electrode placements vary between participants and arrays may come from different batches per the [[Cleanroom Fabrication Process]].

### Next Steps

- Expand to all available participants (currently 5)
- Evaluate if [[Motor Cortex Channel Selection Strategy]] can identify a canonical subset that generalizes across participants
- Coordinate with [[Neurorehab Therapy Suite]] team on their [[Motor Recovery Progress Tracking]] data which captures longitudinal neural changes

#generalization #transfer-learning #multi-participant
`,
	},
	{
		Title:   "TypeScript Client SDK Design",
		Project: 1,
		Tags:    []string{"sdk", "typescript", "architecture"},
		Body: `## TypeScript Client SDK Design for NeuroLink

The TypeScript client SDK is the primary interface for web and Node.js applications integrating with the NeuroLink platform. This note details the design decisions and module structure.

### Module Structure

    @neurolink/sdk
        /core           // Connection, auth, config
        /streams        // Real-time neural data streams
        /devices        // Device management and discovery
        /decode         // Decoder configuration and control
        /storage        // Session recording and retrieval

This aligns with the overall [[SDK Architecture Overview]] and implements the endpoints defined in the [[REST API Specification]].

### Key Design Decisions

**1. Observable-based streaming**
Neural data streams use RxJS Observables rather than raw WebSockets. This enables composable stream processing:

    const hga = client.streams.subscribe('high-gamma', {
        channels: [0, 1, 2, 3],
        downsample: 100  // Hz
    });

    hga.pipe(
        bufferTime(50),
        map(frames => extractFeatures(frames))
    ).subscribe(features => updateVisualization(features));

**2. Automatic reconnection**
The SDK handles WebSocket disconnections transparently with exponential backoff, critical for long clinical sessions.

**3. Type-safe API layer**
All REST endpoints from [[REST API Specification]] have strongly-typed request/response interfaces generated from OpenAPI schemas.

### Authentication

OAuth2 with PKCE flow for web clients, API key auth for server-to-server. Token refresh is automatic. All auth flows comply with [[HIPAA Compliance Checklist]] requirements for session management.

### Data Format

Neural data is transmitted as binary ArrayBuffer with a compact header (see [[Real-Time Telemetry Dashboard]] for the wire format). The SDK handles deserialization into typed Float32Arrays.

### Status

Core module and auth are complete. Streams module is in beta testing per the [[Beta Program Rollout Plan]].

#typescript #sdk-design #client-library
`,
	},
	{
		Title:   "Python Client SDK Design",
		Project: 1,
		Tags:    []string{"sdk", "python", "architecture"},
		Body: `## Python Client SDK for NeuroLink

The Python SDK targets data scientists and researchers who need programmatic access to NeuroLink for offline analysis, model training, and scripting automation.

### Design Philosophy

The Python SDK wraps the same [[REST API Specification]] as the TypeScript client but provides a more Pythonic interface with pandas/numpy integration.

### Core API

    import neurolink

    client = neurolink.Client(
        host="https://lab-bci-01.local",
        api_key="nlk_..."
    )

    # Fetch session data as numpy arrays
    session = client.sessions.get("ses_01HQ3...")
    neural_data = session.neural_data(
        channels=range(64),
        frequency_band="high-gamma"
    )  # returns np.ndarray, shape (n_timepoints, 64)

    # Stream real-time data
    async for frame in client.streams.subscribe("raw-ecog"):
        process(frame)

### Integration with Scientific Stack

- **NumPy**: all array data returned as ndarrays with appropriate dtypes
- **pandas**: session metadata and trial tables returned as DataFrames
- **MNE-Python**: export to MNE Raw objects for standard EEG/ECoG analysis
- **PyTorch**: DataLoader-compatible dataset class for model training via [[Training Data Pipeline]]

### Package Structure

    neurolink/
        __init__.py
        client.py          # Client class, auth, config
        streams.py         # WebSocket streaming
        sessions.py        # Session CRUD
        devices.py         # Device management
        decode.py          # Decoder control
        types.py           # Pydantic models
        _compat.py         # numpy/pandas helpers

### Authentication

Same OAuth2/API key mechanisms as the TypeScript SDK, consistent with [[SDK Architecture Overview]]. Credentials can be stored in environment variables or a config file at ~/.neurolink/config.toml.

### Testing

Unit tests mock the HTTP layer. Integration tests run against a local dev server instance. This is coordinated with the [[Beta Program Rollout Plan]] for external tester access.

#python #sdk-design #scientific-computing
`,
	},
	{
		Title:   "WebSocket Streaming Protocol",
		Project: 1,
		Tags:    []string{"websocket", "protocol", "real-time"},
		Body: `## WebSocket Streaming Protocol for Real-Time Neural Data

The NeuroLink SDK provides real-time neural data streaming over WebSocket connections. This note specifies the wire protocol.

### Connection Lifecycle

1. Client authenticates via REST (see [[REST API Specification]])
2. Client opens WebSocket at /api/v1/stream with auth token
3. Client sends subscription messages for desired data channels
4. Server pushes binary frames at the configured sample rate
5. Client sends keepalive pings every 30 seconds

### Frame Format

Binary frames use a compact format to minimize bandwidth:

    Offset  Size   Field
    0       1      Frame type (0x01=neural, 0x02=event, 0x03=state)
    1       4      Timestamp (uint32, ms since session start)
    5       2      Channel count (uint16)
    7       2      Samples per channel (uint16)
    9       N*M*4  Data (float32, channel-major order)

### Subscription Message

    {
        "action": "subscribe",
        "stream": "high-gamma",
        "channels": [0, 1, 2, 3],
        "sample_rate": 100,
        "format": "float32"
    }

### Bandwidth Estimates

| Configuration         | Channels | Rate (Hz) | Bandwidth (Mbps) |
|-----------------------|----------|-----------|-------------------|
| Raw ECoG              | 256      | 30000     | 245.8             |
| Raw ECoG (subset)     | 64       | 30000     | 61.4              |
| High-gamma features   | 64       | 100       | 0.2               |
| Decoded kinematics    | 4        | 20        | 0.003             |

### Backpressure Handling

If the client cannot keep up, the server applies a configurable policy per the [[SDK Architecture Overview]]:
- **Drop oldest**: discard frames from the head of the buffer
- **Drop newest**: discard incoming frames (preserves temporal continuity)
- **Pause stream**: stop sending until client catches up (sends resume signal)

### Relation to Other Systems

This protocol is consumed by the [[Real-Time Telemetry Dashboard]] for live monitoring and by the [[Closed-Loop Grasp Controller]] for real-time prosthetic control. The wire format is also used by the [[Implant Firmware OTA Update Protocol]] for firmware-level data capture.

#websocket #streaming #wire-protocol
`,
	},
	{
		Title:   "Device Discovery and Pairing API",
		Project: 1,
		Tags:    []string{"api", "device-management", "bluetooth"},
		Body: `## Device Discovery and Pairing in the NeuroLink SDK

The SDK must support discovering and pairing with BCI hardware devices over multiple transports (USB, Bluetooth LE, Wi-Fi Direct). This note describes the device management API.

### Discovery Flow

    // TypeScript example
    const scanner = client.devices.scan({
        transport: ['ble', 'usb'],
        timeout: 10000  // ms
    });

    scanner.on('discovered', (device) => {
        console.log(device.name, device.serial, device.rssi);
    });

    // Pair with a specific device
    const paired = await client.devices.pair(device.id, {
        securityLevel: 'encrypted'
    });

### Supported Devices

| Device Type            | Transport | Protocol | Status    |
|------------------------|-----------|----------|-----------|
| NeuroLink Hub v2       | USB/BLE   | NLP-2.1  | Supported |
| NeuroLink Wireless TX  | BLE       | NLP-2.1  | Supported |
| ECoG Amplifier (g.tec) | USB       | g.API    | Beta      |
| Consumer EEG           | BLE       | Custom   | Planned   |

The consumer EEG integration is planned for compatibility with the [[Non-Invasive EEG Headset]] project's hardware, specifically their [[Dry Electrode Contact Optimization]] configuration.

### Security

Device pairing uses mutual authentication with device certificates. The pairing key is derived via ECDH key exchange and stored in the platform keychain. This meets the security requirements in [[HIPAA Compliance Checklist]].

### Firmware Updates

The SDK exposes firmware update capability via the [[Implant Firmware OTA Update Protocol]]:

    const update = await client.devices.checkFirmwareUpdate(device.id);
    if (update.available) {
        await client.devices.applyFirmwareUpdate(device.id, {
            onProgress: (pct) => console.log(pct + '% complete')
        });
    }

### Architecture Alignment

The device layer sits below the stream layer in the [[SDK Architecture Overview]]. All device communication is abstracted behind a transport-agnostic interface so that higher layers (streaming, decoding) are transport-independent.

#device-management #pairing #ble
`,
	},
	{
		Title:   "SDK Error Handling and Retry Strategy",
		Project: 1,
		Tags:    []string{"sdk", "error-handling", "reliability"},
		Body: `## Error Handling and Retry Strategy in NeuroLink SDK

Robust error handling is critical for clinical applications where SDK failures could interrupt a participant session. This note documents our error taxonomy and retry policies.

### Error Categories

    enum NeuroLinkErrorCode {
        // Connection errors (1xxx)
        CONNECTION_TIMEOUT      = 1001,
        CONNECTION_REFUSED      = 1002,
        WEBSOCKET_CLOSED        = 1003,
        AUTHENTICATION_FAILED   = 1004,

        // Device errors (2xxx)
        DEVICE_NOT_FOUND        = 2001,
        DEVICE_DISCONNECTED     = 2002,
        FIRMWARE_INCOMPATIBLE   = 2003,

        // Data errors (3xxx)
        STREAM_BUFFER_OVERFLOW  = 3001,
        INVALID_CHANNEL_INDEX   = 3002,
        DECODE_TIMEOUT          = 3003,

        // Server errors (5xxx)
        SERVER_INTERNAL_ERROR   = 5001,
        RATE_LIMITED            = 5002,
    }

### Retry Policies

| Error Category   | Retry | Strategy                  | Max Attempts | Backoff     |
|------------------|-------|---------------------------|--------------|-------------|
| Connection       | Yes   | Exponential + jitter      | 10           | 100ms-30s   |
| Auth failure     | No    | Fail immediately          | 0            | N/A         |
| Device disconnect| Yes   | Linear with reconnect     | 5            | 2s fixed    |
| Stream overflow  | Yes   | Resubscribe               | 3            | 500ms fixed |
| Rate limit       | Yes   | Respect Retry-After       | 5            | Server-set  |
| Server error     | Yes   | Exponential               | 3            | 1s-10s      |

### Circuit Breaker

After max retry attempts are exhausted, the SDK enters a circuit-breaker state for that subsystem. The [[REST API Specification]] health endpoint is polled every 30 seconds. When the health check succeeds, the circuit resets.

### Clinical Session Protection

During active clinical sessions (as flagged by the session state), the SDK employs more aggressive retry strategies and maintains a local buffer of neural data that can be replayed when connection is restored. This ensures no data loss per the requirements in [[Adverse Event Reporting SOP]] which requires continuous data capture during trials.

### Logging and Observability

All errors and retries are logged with structured metadata compatible with the [[Real-Time Telemetry Dashboard]]. The [[Beta Program Rollout Plan]] includes error telemetry collection from beta testers (with consent).

#error-handling #retry #reliability
`,
	},
	{
		Title:   "SDK Plugin Architecture",
		Project: 1,
		Tags:    []string{"sdk", "plugins", "extensibility"},
		Body: `## NeuroLink SDK Plugin Architecture

The SDK supports plugins to extend functionality without bloating the core package. This is essential for accommodating diverse research needs across projects.

### Plugin Interface

    interface NeuroLinkPlugin {
        name: string;
        version: string;
        init(client: NeuroLinkClient): void;
        destroy(): void;
    }

    // Registration
    const client = new NeuroLinkClient({
        plugins: [
            new GraspControlPlugin(),
            new SpeechDecoderPlugin(),
            new TelemetryPlugin(),
        ]
    });

### Core Plugin Types

**1. Decoder Plugins** -- add custom decoding algorithms
- Kalman filter decoder (built-in)
- Neural network decoder (via [[Transformer Decoder Architecture]])
- Phoneme decoder (for [[Phoneme Decoder Architecture]])

**2. Device Plugins** -- support new hardware
- Standard NeuroLink hardware (built-in)
- Consumer EEG headsets ([[Dry Electrode Contact Optimization]])
- Third-party amplifiers

**3. Analysis Plugins** -- add processing capabilities
- Spike sorting ([[Spike Sorting Algorithm Comparison]])
- Spectral analysis
- Connectivity metrics

### Plugin Lifecycle

1. **Registration** -- plugin is added to client config
2. **Initialization** -- plugin receives client reference, registers event handlers
3. **Active** -- plugin processes data, may add methods to client
4. **Teardown** -- plugin cleans up resources, unregisters handlers

### SDK Architecture Integration

Plugins hook into the event system described in the [[SDK Architecture Overview]]. They can:
- Intercept and transform streaming data
- Add new REST endpoints to the client
- Register custom device transport handlers
- Extend the client with new methods

### Example: Grasp Controller Plugin

This plugin wraps the [[Closed-Loop Grasp Controller]] functionality:

    const grasp = client.getPlugin<GraspControlPlugin>('grasp-control');
    grasp.on('contact', (event) => {
        console.log('Contact force:', event.force, 'N');
    });
    await grasp.setAperture(0.5);  // 50% open

### Distribution

Plugins are distributed as npm packages under the @neurolink scope. The [[Beta Program Rollout Plan]] includes a plugin marketplace for community contributions.

#plugins #extensibility #architecture
`,
	},
	{
		Title:   "Batch Data Export API",
		Project: 1,
		Tags:    []string{"api", "data-export", "compliance"},
		Body: `## Batch Data Export API for Research and Regulatory Submissions

Researchers and regulatory bodies need bulk access to session data in standardized formats. This note describes the batch export API.

### Supported Export Formats

| Format      | Use Case                              | File Extension |
|-------------|---------------------------------------|---------------|
| NWB 2.0     | Neuroscience research standard         | .nwb          |
| EDF+        | Clinical EEG/ECoG standard             | .edf          |
| CSV         | Simple tabular data                    | .csv          |
| Parquet     | Large-scale analytics                  | .parquet      |
| BIDS        | Brain Imaging Data Structure           | directory     |

### API Design

    // REST endpoint
    POST /api/v1/exports
    {
        "sessions": ["ses_01HQ3...", "ses_01HQ4..."],
        "format": "nwb",
        "channels": [0, 1, 2, 3],
        "time_range": {"start": 0, "end": 3600},
        "include_metadata": true,
        "anonymize": true
    }

    // Response
    {
        "export_id": "exp_01HQ5...",
        "status": "processing",
        "estimated_completion": "2025-03-15T10:30:00Z"
    }

### Anonymization

When the anonymize flag is set, the export pipeline applies the [[Neural Data Anonymization]] protocol:
- Strip participant identifiers
- Randomize timestamps relative to session start
- Remove any free-text notes that might contain PHI
- Generate a de-identification log per [[HIPAA Compliance Checklist]]

### Regulatory Exports

For the [[FDA 510k Submission Timeline]], we need exports in a specific format that includes:
- Raw neural data in EDF+ format
- Decoded kinematics with confidence scores
- Session metadata and decoder parameters
- Adverse event markers (cross-referenced with [[Adverse Event Reporting SOP]])

### Implementation

Exports are processed asynchronously on the [[BCI Cloud Platform]]. Large exports (>10 GB) are chunked and uploaded to a presigned S3 URL. The [[REST API Specification]] includes polling and webhook notification endpoints for export completion.

### SDK Integration

Both the [[TypeScript Client SDK Design]] and [[Python Client SDK Design]] expose this API with helper methods for common export patterns.

#data-export #compliance #nwb
`,
	},
	{
		Title:   "SDK Performance Benchmarks",
		Project: 1,
		Tags:    []string{"performance", "benchmarks", "optimization"},
		Body: `## NeuroLink SDK Performance Benchmarks

This note documents the performance benchmarks for the NeuroLink SDK across different configurations and platforms.

### Test Environment

- **Server**: NeuroLink Hub v2, 256-channel ECoG
- **Client (Node.js)**: M2 MacBook Pro, Node.js 22, localhost
- **Client (Browser)**: Chrome 130, same machine
- **Client (Python)**: Python 3.12, same machine

### Streaming Throughput

| SDK      | Channels | Rate (Hz) | Throughput (MB/s) | CPU Usage (%) | Latency p99 (ms) |
|----------|----------|-----------|-------------------|---------------|-------------------|
| TS/Node  | 256      | 1000      | 1.024             | 8.2           | 3.1               |
| TS/Node  | 64       | 100       | 0.025             | 0.4           | 1.2               |
| TS/Web   | 256      | 1000      | 1.024             | 14.7          | 5.8               |
| TS/Web   | 64       | 100       | 0.025             | 1.1           | 2.3               |
| Python   | 256      | 1000      | 1.024             | 12.3          | 4.5               |
| Python   | 64       | 100       | 0.025             | 0.8           | 1.8               |

### REST API Latency

Measured against the [[REST API Specification]] endpoints:

| Endpoint              | Method | TS p50 (ms) | TS p99 (ms) | Python p50 (ms) | Python p99 (ms) |
|-----------------------|--------|-------------|-------------|-----------------|-----------------|
| /sessions             | GET    | 12          | 28          | 15              | 34              |
| /sessions/:id/data   | GET    | 45          | 120         | 52              | 135             |
| /decode/start         | POST   | 8           | 22          | 11              | 28              |
| /devices              | GET    | 6           | 14          | 8               | 18              |

### Memory Usage

For a 60-minute streaming session with 64 channels at 100 Hz:
- TypeScript (Node): 85 MB steady-state
- TypeScript (Browser): 120 MB steady-state
- Python: 95 MB steady-state

### Optimization Notes

- Binary WebSocket frames ([[WebSocket Streaming Protocol]]) reduced bandwidth by 73% vs JSON encoding
- Connection pooling reduced REST latency by 40%
- The [[SDK Architecture Overview]] specifies a maximum p99 latency of 10 ms for decoded kinematics streams -- we meet this target

### Known Bottlenecks

- Browser garbage collection pauses cause occasional latency spikes >10 ms at high channel counts
- Python asyncio event loop contention under heavy streaming load
- Large batch exports ([[Batch Data Export API]]) can saturate network for several minutes

#performance #benchmarks #latency
`,
	},
	{
		Title:   "SDK Authentication and Token Management",
		Project: 1,
		Tags:    []string{"authentication", "security", "oauth"},
		Body: `## Authentication and Token Management in NeuroLink SDK

Secure authentication is non-negotiable for a medical device platform. This note describes the SDK's auth implementation.

### Authentication Methods

**1. API Key (Server-to-Server)**

    const client = new NeuroLinkClient({
        apiKey: process.env.NEUROLINK_API_KEY
    });

API keys are scoped to specific permissions (read, write, admin) and bound to specific device serials. Keys are rotated every 90 days per [[HIPAA Compliance Checklist]].

**2. OAuth2 + PKCE (Web/Mobile)**

    const client = new NeuroLinkClient({
        auth: {
            type: 'oauth2-pkce',
            clientId: 'app_01HQ3...',
            redirectUri: 'https://app.example.com/callback',
            scopes: ['streams:read', 'sessions:write']
        }
    });

### Token Lifecycle

    Token Request -> Access Token (15 min) + Refresh Token (7 days)
                     |                        |
                     v                        v
                  API Calls              Token Refresh
                     |                        |
                     v                        v
                  Token Expiry           New Access Token
                     |
                     v
                  Auto-Refresh (transparent to caller)

### Permission Scopes

| Scope              | Description                              | Required For                |
|--------------------|------------------------------------------|-----------------------------|
| streams:read       | Subscribe to neural data streams         | Real-time monitoring        |
| streams:write      | Configure stream parameters              | Decoder control             |
| sessions:read      | Read session recordings                  | Data analysis               |
| sessions:write     | Create/modify sessions                   | Clinical recording          |
| devices:manage     | Pair/unpair devices                      | Device setup                |
| admin              | Full access                              | System administration       |

### Clinical Session Tokens

During active clinical sessions for the [[Phase I Trial Design]], the SDK issues a special long-lived session token (8 hours) that cannot be revoked mid-session. This prevents authentication issues from interrupting critical recording sessions.

### Audit Logging

All authentication events are logged per [[HIPAA Compliance Checklist]]:
- Login attempts (success and failure)
- Token refreshes
- Permission scope changes
- API key rotations

Audit logs feed into the [[Real-Time Telemetry Dashboard]] security view. The [[REST API Specification]] includes audit log query endpoints.

#authentication #oauth2 #hipaa
`,
	},
	{
		Title:   "SDK Documentation and Developer Portal",
		Project: 1,
		Tags:    []string{"documentation", "developer-experience", "onboarding"},
		Body: `## Developer Portal and Documentation Strategy

The quality of SDK documentation directly impacts adoption. This note outlines our documentation architecture and developer portal design.

### Documentation Layers

1. **API Reference** -- auto-generated from OpenAPI spec ([[REST API Specification]]) and TypeDoc/Sphinx for SDK classes
2. **Guides** -- step-by-step tutorials for common workflows
3. **Conceptual docs** -- explain BCI concepts for developers new to neuroscience
4. **Cookbook** -- copy-paste code snippets for specific tasks

### Key Guides

| Guide                          | Target Audience         | SDK Module              |
|--------------------------------|-------------------------|-------------------------|
| Getting Started                | All developers          | Core                    |
| Real-Time Streaming            | App developers          | [[WebSocket Streaming Protocol]] |
| Device Setup                   | Lab technicians         | [[Device Discovery and Pairing API]] |
| Data Export for Research       | Data scientists         | [[Batch Data Export API]] |
| Building Decoder Plugins       | Neuroscience researchers| [[SDK Plugin Architecture]] |
| Clinical Session Management    | Clinical staff          | Sessions                |

### Developer Portal Features

- **Interactive API explorer** -- try endpoints with test data
- **SDK playground** -- browser-based code editor with TypeScript SDK pre-loaded
- **Status page** -- real-time service health from [[Real-Time Telemetry Dashboard]]
- **Changelog** -- versioned API and SDK changes

### Code Examples Repository

We maintain a public examples repo with:
- Basic cursor control demo
- Real-time neural signal visualization
- Session recording and playback
- Integration with the [[Game Controller Abstraction Layer]] from the BCI Gaming team
- Speech decoding pipeline using [[Real-Time Speech Synthesis Pipeline]]

### Beta Documentation

Per the [[Beta Program Rollout Plan]], beta testers get early access to documentation with feedback mechanisms. We track documentation gaps through beta tester support tickets.

### Tooling

- TypeScript SDK: TypeDoc with custom theme
- Python SDK: Sphinx with autodoc and Napoleon extension
- API spec: Redoc or Stoplight Elements
- All docs versioned alongside SDK releases per [[SDK Architecture Overview]]

#documentation #developer-portal #onboarding
`,
	},
	{
		Title:   "SDK Versioning and Backwards Compatibility",
		Project: 1,
		Tags:    []string{"versioning", "compatibility", "release-management"},
		Body: `## Versioning and Backwards Compatibility Policy

The NeuroLink SDK must maintain stability for clinical deployments while still evolving rapidly. This note defines our versioning strategy.

### Semantic Versioning

We follow SemVer strictly:
- **Major** (X.0.0): breaking API changes. Require code changes from consumers.
- **Minor** (0.X.0): new features, backwards compatible. Safe to upgrade.
- **Patch** (0.0.X): bug fixes only. Always safe to upgrade.

### Compatibility Matrix

| SDK Version | API Version | Protocol Version | Min Firmware |
|-------------|-------------|------------------|--------------|
| 1.x         | v1          | NLP-2.0          | 3.0.0        |
| 2.x         | v1, v2      | NLP-2.1          | 3.2.0        |
| 3.x (planned)| v2        | NLP-3.0          | 4.0.0        |

### Breaking Change Policy

Before any major version bump:
1. Deprecation warnings in the previous minor version for at least 6 months
2. Migration guide published on the developer portal ([[SDK Documentation and Developer Portal]])
3. Both old and new API versions supported simultaneously for 12 months
4. Clinical sites on active [[Phase I Trial Design]] protocols are never force-upgraded

### API Version Negotiation

The SDK performs version negotiation on connection:

    GET /api/version
    Response: { "versions": ["v1", "v2"], "recommended": "v2" }

The SDK selects the highest mutually supported version, consistent with the [[REST API Specification]].

### Firmware Compatibility

SDK versions are tested against specific firmware versions. The [[Implant Firmware OTA Update Protocol]] ensures devices are updated before SDK upgrades that require newer firmware. The compatibility matrix is enforced at connection time.

### Release Process

1. Feature freeze 2 weeks before release
2. Beta testing per [[Beta Program Rollout Plan]]
3. Release candidate testing with clinical sites
4. Production release with 2-week monitoring period
5. Post-release retrospective

### Regulatory Impact

Per [[EU MDR Classification]], any SDK change that affects the decode pipeline or device communication is considered a software change that may require re-certification. We maintain a change classification system that flags regulatory-impacting changes.

#versioning #semver #compatibility
`,
	},
	{
		Title:   "SDK Rate Limiting and Quota Management",
		Project: 1,
		Tags:    []string{"api", "rate-limiting", "infrastructure"},
		Body: `## Rate Limiting and Quota Management

The NeuroLink platform enforces rate limits to ensure fair resource allocation and system stability. This note documents the rate limiting strategy exposed through the SDK.

### Rate Limit Tiers

| Tier         | REST (req/min) | Stream Subscriptions | Export (GB/day) | Use Case           |
|--------------|----------------|----------------------|------------------|--------------------|
| Research     | 600            | 5                    | 50               | Academic labs      |
| Clinical     | 1200           | 10                   | 200              | Clinical trials    |
| Enterprise   | 6000           | 50                   | 1000             | Commercial partners|
| Internal     | Unlimited      | Unlimited            | Unlimited        | Internal testing   |

### SDK Behavior

The SDK handles rate limits transparently:

    // Rate limit headers
    X-RateLimit-Limit: 600
    X-RateLimit-Remaining: 423
    X-RateLimit-Reset: 1710500000

    // SDK auto-retry on 429
    client.config.rateLimitRetry = true;  // default: true
    client.config.rateLimitRetryMax = 3;

When a 429 response is received, the SDK follows the retry strategy documented in [[SDK Error Handling and Retry Strategy]].

### Quota Tracking

The SDK exposes quota information:

    const quota = await client.quota.get();
    // {
    //   tier: 'clinical',
    //   rest: { used: 177, limit: 1200, resetsAt: '...' },
    //   streams: { active: 3, limit: 10 },
    //   export: { usedGB: 12.4, limitGB: 200, resetsAt: '...' }
    // }

### Clinical Session Exemptions

Active clinical recording sessions tied to the [[Phase I Trial Design]] are exempt from rate limiting on stream subscriptions and real-time endpoints. This is enforced via the session token mechanism described in [[SDK Authentication and Token Management]].

### Multi-Site Considerations

For labs running the [[Multi-Site Data Sync Architecture]], rate limits are applied per-site rather than per-account. This prevents one busy site from starving others.

### Monitoring

Rate limit utilization is tracked on the [[Real-Time Telemetry Dashboard]]. Alerts fire when any consumer consistently hits >80% of their quota.

#rate-limiting #quota #api-management
`,
	},
	{
		Title:   "Micro-stimulation Waveform Library",
		Project: 2,
		Tags:    []string{"stimulation", "waveforms", "parameter-space"},
		Body: `## Micro-stimulation Waveform Library for Sensory Feedback

This note catalogs the stimulation waveforms available for evoking tactile percepts through intracortical micro-stimulation (ICMS) in somatosensory cortex (S1).

### Waveform Types

All waveforms are charge-balanced to prevent tissue damage. Parameters are constrained to the safe ranges defined in [[Micro-stimulation Parameter Space]].

**1. Biphasic Symmetric**
- Cathodic-first, symmetric phases
- Duration: 100-400 us per phase
- Amplitude: 10-100 uA
- Most commonly used, well-characterized perceptual quality

**2. Biphasic Asymmetric**
- Short cathodic pulse, longer anodic return
- Cathodic: 100 us, Anodic: 400 us (1:4 ratio)
- Lower perceptual threshold, reduced tissue activation volume

**3. Triphasic**
- Pre-pulse (anodic) + main pulse (cathodic) + charge balance (anodic)
- Better artifact rejection for concurrent recording
- Used during closed-loop operation with [[Closed-Loop Grasp Controller]]

**4. Pulse Train Modulation**
- Fixed waveform shape, modulate frequency (50-300 Hz) and amplitude
- Frequency encodes stimulus intensity
- Amplitude encodes spatial extent

### Perceptual Quality Mapping

| Waveform   | Frequency (Hz) | Typical Percept          | Body Location (S1 map) |
|------------|-----------------|--------------------------|------------------------|
| Biphasic   | 50-100         | Pressure, touch          | Fingertip              |
| Biphasic   | 100-200        | Vibration, flutter       | Palm                   |
| Biphasic   | 200-300        | Buzzing, tingling        | Varies                 |
| Triphasic  | 50-100         | Natural pressure         | Fingertip              |

### Integration

Waveform selection is exposed through the [[Sensory Feedback Integration API]] and controlled by the [[Closed-Loop Grasp Controller]] based on decoded contact forces. The electrode safety limits are validated against [[Electrode Impedance QC Protocol]] measurements before each session.

#waveforms #micro-stimulation #tactile-feedback
`,
	},
	{
		Title:   "Somatotopic Mapping Procedure",
		Project: 2,
		Tags:    []string{"mapping", "somatosensory", "protocol"},
		Body: `## Somatotopic Mapping of Stimulation Electrodes

Before delivering meaningful sensory feedback, we must map which electrodes in S1 evoke percepts at which body locations. This note documents the mapping procedure.

### Pre-Mapping Setup

1. Verify electrode impedances are within range per [[Electrode Impedance QC Protocol]]
2. Load the safe stimulation parameter bounds from [[Micro-stimulation Parameter Space]]
3. Confirm stimulator hardware communication via [[Sensory Feedback Integration API]]

### Mapping Protocol

**Phase 1: Threshold Detection**
For each electrode (up to 64 channels on the S1 array):
- Start at minimum amplitude (10 uA)
- Deliver 1-second pulse trains at 100 Hz using biphasic waveform from [[Micro-stimulation Waveform Library]]
- Increase amplitude in 5 uA steps
- Record detection threshold (participant reports "I feel something")
- Record comfortable maximum (participant reports intensity 7/10)

**Phase 2: Localization**
For each electrode above threshold:
- Stimulate at 1.5x threshold amplitude
- Participant reports perceived body location on a diagram
- Repeat 3 times per electrode to assess consistency

**Phase 3: Quality Characterization**
For consistent electrodes (same location 3/3 trials):
- Sweep frequency from 50-300 Hz
- Participant rates percept quality (natural, tingling, buzzing, painful)
- Record preferred parameters

### Data Format

    type SomatotopicMap struct {
        ElectrodeID    int
        Threshold_uA   float64
        MaxComfort_uA   float64
        BodyLocation   string    // e.g., "index_fingertip_L"
        Consistency    int       // out of 3
        PreferredFreq  float64
        PerceptQuality string
    }

### Results (Current Participant)

Of 64 S1 electrodes:
- 48 evoked detectable percepts
- 39 mapped consistently to a single body location
- 12 fingertip locations, 8 palm, 11 dorsal hand, 8 wrist/forearm

### Clinical Relevance

The somatotopic map is essential for the [[Phase I Trial Design]] outcome measures. It is also used by the [[Closed-Loop Grasp Controller]] to deliver spatially appropriate feedback during object manipulation. Maps are updated monthly and changes are reported per [[IRB Protocol Amendments]].

#somatotopy #mapping #sensory-feedback
`,
	},
	{
		Title:   "Force-to-Stimulation Transfer Function",
		Project: 2,
		Tags:    []string{"transfer-function", "force-feedback", "control"},
		Body: `## Mapping Contact Force to Stimulation Parameters

When a prosthetic hand grasps an object, the contact force sensors generate continuous force signals that must be converted to stimulation parameters. This note defines the transfer function.

### Design Requirements

1. Force range: 0-20 N (fingertip grasp range)
2. Stimulation must stay within safe bounds from [[Micro-stimulation Parameter Space]]
3. Perceptual intensity should scale monotonically with force
4. Latency from force sensor to stimulation onset < 10 ms

### Transfer Function

We use a logarithmic mapping from force to stimulation amplitude, matching the Weber-Fechner psychophysical law:

    stim_amplitude = A_min + (A_max - A_min) * log(1 + k*force) / log(1 + k*F_max)

Where:
- A_min = detection threshold (electrode-specific, from [[Somatotopic Mapping Procedure]])
- A_max = comfortable maximum (electrode-specific)
- k = 5.0 (shape parameter, tuned empirically)
- F_max = 20 N

### Frequency Modulation

In addition to amplitude, we modulate stimulation frequency to encode force magnitude:

| Force Range (N) | Frequency (Hz) | Percept           |
|-----------------|-----------------|-------------------|
| 0-2             | 50              | Light touch       |
| 2-8             | 100             | Firm grasp        |
| 8-15            | 200             | Strong grip       |
| 15-20           | 300             | Near-maximum      |

### Integration with Grasp Controller

The [[Closed-Loop Grasp Controller]] sends force readings to the stimulation controller at 1 kHz. The transfer function is applied per-electrode based on the somatotopic map. The combined force + stimulation loop achieves a total latency of 6.2 ms.

### Calibration

The transfer function parameters (k, A_min, A_max) are calibrated per participant using psychophysical just-noticeable-difference (JND) testing. We run calibration at the start of each session, taking approximately 5 minutes.

### Safety Bounds

The transfer function output is hard-clamped to never exceed the charge density limit of 30 uC/cm^2/phase, consistent with [[Electrode Impedance QC Protocol]] safety margins. An independent hardware watchdog enforces this limit regardless of software state.

#force-feedback #transfer-function #psychophysics
`,
	},
	{
		Title:   "Bidirectional BCI Timing Synchronization",
		Project: 2,
		Tags:    []string{"timing", "synchronization", "bidirectional"},
		Body: `## Timing Synchronization for Bidirectional BCI

The sensory feedback loop requires precise timing coordination between the recording system (cortical decoder) and the stimulation system. Decode and stimulate must not overlap on shared electrodes, and feedback latency must be minimized.

### The Timing Problem

Our BCI is bidirectional: we record from M1 for motor decoding and stimulate in S1 for sensory feedback. Although these use separate electrode arrays, stimulation artifacts can propagate to recording electrodes. We must:

1. Blank recording channels during stimulation pulses
2. Maintain decode throughput despite blanking
3. Keep the total sensory feedback latency under 20 ms

### Timing Architecture

    Record Cycle:    |--sample--|--sample--|--BLANK--|--sample--|--sample--|--BLANK--|
    Stim Cycle:      |----------|----------|--STIM---|----------|----------|--STIM---|
    Time (ms):       0          1          2         3          4          5

Recording runs at 30 kHz (33 us per sample). Stimulation pulses are 200 us. We blank a 500 us window around each stimulation pulse, losing ~1.5% of recording samples.

### Artifact Rejection

Even with blanking, residual artifacts can persist. We apply:

1. **Template subtraction** -- average artifact template computed from the first 10 stimulation pulses, then subtracted from subsequent epochs
2. **Interpolation** -- linear interpolation across the blanked window for continuous feature extraction
3. **Frequency domain filtering** -- stimulation artifacts have known spectral signatures that can be notch-filtered

### Synchronization Protocol

Both systems are clocked from a shared 10 MHz reference. The stimulator sends a trigger pulse on each stimulation event. The recording system timestamps this trigger to align blanking windows. This is managed through the [[Sensory Feedback Integration API]].

### Integration Points

- The [[ECoG Signal Preprocessing Pipeline]] must be aware of blanking windows and not interpret them as signal dropout
- The [[Wireless Transmitter Power Budget]] analysis must account for the additional power draw of the stimulator
- The [[RF Link Budget Analysis]] from the Implant Telemetry team must consider potential EMI from stimulation pulses on the wireless link

### Verification

Timing synchronization is verified at the start of each session using a loopback test. The [[Real-Time Telemetry Dashboard]] displays real-time sync status and alerts on drift > 10 us.

#timing #synchronization #bidirectional-bci
`,
	},
	{
		Title:   "Grasp Type Classification for Feedback Selection",
		Project: 2,
		Tags:    []string{"classification", "grasp", "feedback"},
		Body: `## Classifying Grasp Type to Select Appropriate Sensory Feedback Patterns

Different grasp types activate different hand regions and require different spatial patterns of sensory feedback. This note describes the grasp classifier that drives feedback electrode selection.

### Grasp Taxonomy

| Grasp Type     | Description              | Active Regions        | Feedback Electrodes |
|----------------|--------------------------|----------------------|---------------------|
| Precision pinch| Thumb + index fingertip  | D1, D2 fingertips   | 4 electrodes        |
| Power grasp    | Full hand wrap           | All digits + palm    | 12 electrodes       |
| Lateral pinch  | Thumb + index side       | D1 tip, D2 lateral  | 3 electrodes        |
| Tripod         | Thumb + index + middle   | D1, D2, D3 tips     | 6 electrodes        |
| Spherical      | All fingers, spread      | All digit tips       | 10 electrodes       |

### Classification Approach

The grasp type is inferred from the decoded hand kinematics provided by the decoder (see [[Two-Dimensional vs Three-Dimensional Decoding]] for the 3D extension work). We use a simple rule-based classifier:

    func ClassifyGrasp(aperture float64, fingerAngles [5]float64) GraspType {
        if aperture < 0.2 && fingerAngles[0] > 60 && fingerAngles[1] > 60 {
            return PrecisionPinch
        }
        if aperture < 0.3 && allAbove(fingerAngles, 45) {
            return PowerGrasp
        }
        // ... additional rules
    }

### Feedback Pattern Generation

Once the grasp type is classified, the [[Closed-Loop Grasp Controller]] activates the corresponding electrode subset from the [[Somatotopic Mapping Procedure]]. Force feedback is distributed across active electrodes using the [[Force-to-Stimulation Transfer Function]].

### Transition Handling

When the grasp type changes (e.g., switching from power grasp to precision pinch), we ramp stimulation down on deactivating electrodes over 100 ms and ramp up on activating electrodes over 100 ms to prevent jarring percept transitions.

### Relation to Other Projects

The [[Low-Latency Intent Classification]] work on the BCI Gaming Platform uses similar classification techniques for game control. We are exploring whether their model could be adapted for grasp classification. The [[Adaptive Difficulty Algorithm]] from Neurorehab could also inform progressive introduction of grasp types during training.

#grasp-classification #feedback-patterns #hand-control
`,
	},
	{
		Title:   "Proprioceptive Feedback Encoding Strategy",
		Project: 2,
		Tags:    []string{"proprioception", "encoding", "joint-position"},
		Body: `## Encoding Joint Position as Proprioceptive Feedback

Beyond contact force (exteroception), participants need proprioceptive feedback -- awareness of limb position -- for effective prosthetic control. This note describes our approach to encoding joint angles as stimulation patterns.

### Why Proprioception Matters

Without proprioceptive feedback, participants must visually monitor the prosthetic hand at all times. This is cognitively exhausting and prevents natural, fluid movement. The [[Closed-Loop Grasp Controller]] currently provides only force feedback; adding proprioception is the next priority.

### Encoding Scheme

We encode joint angle using stimulation frequency on electrodes mapped to deep tissue percepts (as identified during [[Somatotopic Mapping Procedure]]):

    Joint Angle (degrees) -> Stimulation Frequency (Hz)

    0 (fully open)   -> 50 Hz  (slow, light tingling)
    45                -> 100 Hz
    90                -> 150 Hz
    135               -> 200 Hz
    180 (fully closed)-> 250 Hz

The amplitude is held constant at 1.5x detection threshold. This creates a distinguishable frequency gradient that participants can learn to interpret as joint position.

### Multi-Joint Encoding

For a hand with 5 DOF (MCP flexion for each digit), we need 5 independent proprioceptive channels. This requires electrodes with non-overlapping percept fields, which is challenging given the limited electrode count.

**Strategy: Multiplexed proprioception**
- Cycle through digits, stimulating each for 200 ms in round-robin
- Complete cycle: 1 second for all 5 digits
- Participant perceives continuous proprioceptive awareness after training

### Training Protocol

Participants undergo a 5-session training regimen:
1. Sessions 1-2: Single joint angle discrimination (8 angles)
2. Sessions 3-4: Two-joint concurrent discrimination
3. Session 5: Full hand proprioceptive integration with [[Closed-Loop Grasp Controller]]

### Preliminary Results

After training, participants can discriminate joint angles with a mean absolute error of 18 degrees (compared to 45+ degrees without proprioceptive feedback).

### Related Work

The [[Lumbar Electrode Placement Protocol]] from the Spinal Cord Interface team faces similar proprioceptive encoding challenges for lower-limb feedback. Their [[Locomotion Decoder Design]] also requires joint angle estimation.

#proprioception #joint-angle #encoding
`,
	},
	{
		Title:   "Stimulation Safety Monitoring System",
		Project: 2,
		Tags:    []string{"safety", "monitoring", "compliance"},
		Body: `## Real-Time Stimulation Safety Monitoring

Electrical stimulation of neural tissue carries inherent risks. This note documents the multi-layered safety monitoring system for the sensory feedback loop.

### Safety Parameters

All stimulation must remain within the bounds defined in [[Micro-stimulation Parameter Space]]:

| Parameter              | Limit                        | Monitoring Rate |
|-----------------------|------------------------------|-----------------|
| Charge per phase       | < 30 uC/cm^2                | Per pulse       |
| Charge balance         | Net charge < 1 nC per 1000 pulses | Per second |
| Maximum amplitude      | 100 uA per electrode         | Per pulse       |
| Maximum frequency      | 300 Hz per electrode         | Per second      |
| Duty cycle             | < 50% per electrode          | Per second      |
| Total charge per hour  | < 10 mC across all channels  | Per minute      |

### Monitoring Layers

**Layer 1: Hardware watchdog**
- Independent microcontroller monitors charge output
- Hard-disconnects stimulator if any limit exceeded
- Cannot be overridden by software
- Tested per [[Electrode Impedance QC Protocol]]

**Layer 2: Firmware limits**
- Stimulator firmware enforces per-pulse charge limits
- Updated via [[Implant Firmware OTA Update Protocol]]

**Layer 3: Software monitoring**
- Real-time tracking of cumulative charge, frequency, and duty cycle
- Exposed through [[Sensory Feedback Integration API]]
- Alerts displayed on [[Real-Time Telemetry Dashboard]]

**Layer 4: Session-level review**
- Post-session charge delivery report generated automatically
- Reviewed by clinical team before next session
- Anomalies flagged per [[Adverse Event Reporting SOP]]

### Impedance Monitoring

Electrode impedance is measured before and after each stimulation session. A sudden impedance change (>50% within a session) triggers an automatic stimulation halt on the affected electrode. This could indicate electrode damage or tissue changes.

### Regulatory Compliance

The safety monitoring system is documented as a critical safety feature in the [[FDA 510k Submission Timeline]] and [[EU MDR Classification]] submissions. All safety events are logged with timestamps, parameter values, and automatic classification.

### Emergency Shutdown

An emergency stop button is accessible to both the participant and the clinical operator. Activation:
1. Immediately ceases all stimulation
2. Logs the event with full system state
3. Triggers an alert on the telemetry dashboard
4. Generates an incident report for [[IRB Protocol Amendments]]

#safety #stimulation-monitoring #regulatory
`,
	},
	{
		Title:   "Sensory Feedback Psychophysics Experiments",
		Project: 2,
		Tags:    []string{"psychophysics", "perception", "experiments"},
		Body: `## Psychophysics Experiments for Characterizing Sensory Feedback Quality

To optimize the sensory feedback loop, we conduct psychophysics experiments that quantify how well participants perceive and discriminate stimulation-evoked percepts.

### Experiment 1: Detection Threshold

**Protocol**: Single-interval yes/no detection task
- Stimulate at varying amplitudes (method of limits, ascending)
- 50 trials per electrode per session
- Measure threshold as 75% detection probability
- Results feed into [[Somatotopic Mapping Procedure]]

### Experiment 2: Amplitude Discrimination (JND)

**Protocol**: Two-interval forced-choice
- Present reference stimulus then comparison stimulus
- Vary comparison amplitude using adaptive staircase (3-down/1-up)
- JND measured as 79% correct discrimination

**Current Results:**
- Mean JND: 15% of reference amplitude (Weber fraction = 0.15)
- This limits the number of discriminable force levels to ~7 across the comfortable range
- The [[Force-to-Stimulation Transfer Function]] is designed to space force levels by at least 1.5 JND

### Experiment 3: Frequency Discrimination

**Protocol**: Same as Exp 2, but varying frequency

| Reference (Hz) | JND (Hz) | Weber Fraction |
|-----------------|----------|----------------|
| 50              | 12       | 0.24           |
| 100             | 18       | 0.18           |
| 200             | 30       | 0.15           |
| 300             | 52       | 0.17           |

These JNDs inform the frequency encoding in [[Proprioceptive Feedback Encoding Strategy]].

### Experiment 4: Two-Point Discrimination

**Protocol**: Stimulate one or two electrodes, participant reports count
- Measures the effective spatial resolution of the electrode array
- Critical for determining how many independent feedback channels we can use simultaneously

### Data Management

All psychophysics data is stored in the per-participant database and exported via the [[Batch Data Export API]] for statistical analysis. Data is anonymized per [[Neural Data Anonymization]] protocols before publication.

### Clinical Relevance

Psychophysics results are a secondary outcome measure in the [[Phase I Trial Design]]. The [[IRB Protocol Amendments]] specify the maximum number of psychophysics trials per session (200) to manage participant fatigue.

#psychophysics #perception #jnd
`,
	},
	{
		Title:   "Closed-Loop Latency Optimization",
		Project: 2,
		Tags:    []string{"latency", "optimization", "closed-loop"},
		Body: `## Optimizing End-to-End Latency for Closed-Loop Sensory Feedback

The total latency from prosthetic contact to perceived sensation must be under 100 ms for the feedback to feel causally linked to the action. Our target is 50 ms. This note tracks our optimization efforts.

### Current Latency Breakdown

| Stage                          | Current (ms) | Target (ms) | Owner                             |
|--------------------------------|-------------|-------------|-----------------------------------|
| Force sensor sampling          | 1.0         | 1.0         | Prosthetic hardware               |
| Force data transmission        | 3.2         | 2.0         | USB/wireless                      |
| Grasp classification           | 2.1         | 1.5         | [[Grasp Type Classification for Feedback Selection]] |
| Transfer function computation  | 0.3         | 0.3         | [[Force-to-Stimulation Transfer Function]] |
| Stimulation command generation | 1.5         | 1.0         | [[Sensory Feedback Integration API]]|
| Stimulation command transmission| 4.8        | 3.0         | Wireless link                     |
| Stimulator hardware response   | 2.0         | 2.0         | Hardware fixed                    |
| Neural conduction to percept   | ~10         | ~10         | Biology (fixed)                   |
| **Total**                      | **24.9**    | **20.8**    |                                   |

### Optimization Strategies

**1. Predictive stimulation**
Instead of waiting for contact force data, predict contact 50-100 ms before it occurs based on decoded hand trajectory from the motor decoder. The [[Closed-Loop Grasp Controller]] already computes a predicted contact time.

**2. Co-located processing**
Move the stimulation controller firmware to the same implant module as the recording system, eliminating the external processing loop. This requires coordination with [[RF Link Budget Analysis]] and [[Implant Firmware OTA Update Protocol]].

**3. Streamlined communication protocol**
Replace the current JSON command format with a binary protocol matching the efficiency of the [[WebSocket Streaming Protocol]] used by the NeuroLink SDK.

### Measuring Perceived Latency

We use a psychophysical task where participants report the perceived simultaneity of:
- Visual feedback (prosthetic hand touching an object on screen)
- Stimulation feedback (evoked tactile percept)

At 25 ms total latency, 85% of participants report the feedback as simultaneous with the visual event.

### Comparison with Neural Feedback Latency

The motor decode pathway from neural event to cursor update is 37 ms per [[Decoder Latency Budget Breakdown]]. The sensory feedback pathway at 25 ms is actually faster, which is desirable since sensory feedback should feel like an immediate consequence of the motor action.

#latency #optimization #sensory-feedback
`,
	},
	{
		Title:   "Multi-Electrode Spatial Feedback Patterns",
		Project: 2,
		Tags:    []string{"spatial-patterns", "multi-electrode", "perception"},
		Body: `## Spatial Feedback Patterns Using Multi-Electrode Stimulation

Single-electrode stimulation produces point-like percepts. By coordinating stimulation across multiple electrodes, we can create richer spatial patterns that convey shape, texture, and movement information.

### Pattern Types

**1. Contour tracing**
Sequential activation of adjacent electrodes to trace a shape across the skin representation. Used to convey object edges during grasp.

    // Activate electrodes in sequence with 50 ms spacing
    pattern := []StimEvent{
        {Electrode: 12, Delay: 0},
        {Electrode: 13, Delay: 50},
        {Electrode: 14, Delay: 100},
        {Electrode: 15, Delay: 150},
    }

**2. Apparent motion**
Two electrodes activated in rapid succession (30-80 ms ISI) create a percept of movement between the two points. Used to convey slip detection.

**3. Spatial summation**
Multiple electrodes activated simultaneously with graded amplitudes create a spatially distributed percept. Used for large-area pressure feedback.

### Integration with Grasp Controller

The [[Closed-Loop Grasp Controller]] generates spatial patterns based on:
- Object contact geometry from force sensor arrays
- Grasp type from [[Grasp Type Classification for Feedback Selection]]
- Electrode mapping from [[Somatotopic Mapping Procedure]]

### Texture Encoding

Preliminary experiments encode surface texture using temporal modulation patterns on a fixed electrode set:

| Texture   | Pattern                          | Frequency (Hz) | Modulation |
|-----------|----------------------------------|-----------------|------------|
| Smooth    | Constant amplitude               | 100             | None       |
| Rough     | Amplitude modulated at 10-30 Hz  | 200             | AM 30%     |
| Ridged    | Periodic burst (on/off at 5 Hz)  | 150             | Burst      |
| Soft      | Low amplitude, wide spatial      | 80              | None       |

### Challenges

- Maximum simultaneous electrodes is limited by charge density safety constraints from [[Micro-stimulation Parameter Space]]
- Electrode interactions: stimulating adjacent electrodes simultaneously can produce nonlinear perceptual effects
- Participants require 10+ hours of training to reliably interpret complex spatial patterns

### Future Directions

The [[Consumer EEG Signal Quality]] work from the Non-Invasive EEG team suggests that non-invasive haptic feedback (vibrotactile) could supplement ICMS for non-critical feedback channels. We are evaluating a hybrid approach.

#spatial-patterns #multi-electrode #texture
`,
	},
	{
		Title:   "Adaptive Feedback Gain Control",
		Project: 2,
		Tags:    []string{"adaptive", "gain-control", "habituation"},
		Body: `## Adaptive Gain Control for Sensory Feedback

Participants habituate to constant stimulation over time, perceiving it as less intense. This note describes our adaptive gain control system that compensates for perceptual habituation.

### The Habituation Problem

During extended sessions (>30 minutes), participants report that stimulation-evoked percepts gradually decrease in perceived intensity, even with constant stimulation parameters. Psychophysics measurements from [[Sensory Feedback Psychophysics Experiments]] show:

- 10% perceived intensity decrease at 15 minutes
- 25% decrease at 30 minutes
- 40% decrease at 60 minutes (plateau)

This means the [[Force-to-Stimulation Transfer Function]] produces progressively less salient feedback over time.

### Adaptive Gain Algorithm

We implement a slow gain ramp that increases stimulation amplitude to compensate:

    type AdaptiveGain struct {
        BaseGain     float64
        CurrentGain  float64
        HabituationRate float64  // gain increase per minute
        MaxGain      float64     // safety ceiling
        SessionStart time.Time
    }

    func (ag *AdaptiveGain) Update() float64 {
        elapsed := time.Since(ag.SessionStart).Minutes()
        ag.CurrentGain = ag.BaseGain * (1.0 + ag.HabituationRate * math.Log1p(elapsed))
        if ag.CurrentGain > ag.MaxGain {
            ag.CurrentGain = ag.MaxGain
        }
        return ag.CurrentGain
    }

### Safety Constraints

The adaptive gain is bounded:
- MaxGain is set to 1.8x BaseGain (never exceeds 80% of comfortable maximum from [[Somatotopic Mapping Procedure]])
- Total charge delivery is monitored by [[Stimulation Safety Monitoring System]]
- If approaching hourly charge limits, gain increase is paused

### Participant-Specific Calibration

Habituation rates vary across participants and even across electrodes. We calibrate the HabituationRate parameter during the first 3 sessions using intermittent perceptual intensity ratings.

### Rest Periods

Habituation recovers with rest. We recommend 5-minute breaks every 30 minutes, which is documented in the session protocol for [[Phase I Trial Design]]. After a 5-minute break, perceived intensity recovers to approximately 90% of the initial level.

### Connection to Motor Decoding

Interestingly, habituation to sensory feedback can affect motor decoding performance. When feedback becomes less salient, participants rely more on visual feedback, changing their neural control strategies. This is tracked as a covariate in [[Participant P07 Long-Term Stability Report]] analysis.

#habituation #adaptive-gain #perceptual-stability
`,
	},
	{
		Title:   "Sensory Feedback During Sleep and Rest States",
		Project: 2,
		Tags:    []string{"sleep", "safety", "state-detection"},
		Body: `## Managing Sensory Feedback During Sleep and Rest States

A chronic implanted BCI must handle transitions between active use, rest, and sleep. Stimulation during sleep is undesirable and potentially disruptive. This note describes our state detection and feedback management system.

### Cortical State Detection

We classify the participant's cortical state using features from the recording array:

| State     | Delta Power (1-4 Hz) | Beta Power (13-30 Hz) | HGA Power | Classification |
|-----------|----------------------|------------------------|-----------|----------------|
| Active    | Low                  | Moderate               | High      | Stimulation ON |
| Drowsy    | Rising               | Decreasing             | Low       | Stimulation REDUCED |
| Light sleep| High                | Low                    | Very low  | Stimulation OFF |
| Deep sleep | Very high           | Very low               | Minimal   | Stimulation OFF |

The state classifier runs on the same neural features extracted by the [[ECoG Signal Preprocessing Pipeline]], requiring minimal additional computation.

### Feedback State Machine

    Active -> Drowsy:  Ramp stimulation to 50% over 10 seconds
    Drowsy -> Sleep:   Ramp stimulation to 0% over 5 seconds
    Sleep -> Drowsy:   No stimulation change (wait for Active)
    Drowsy -> Active:  Ramp stimulation to 100% over 3 seconds
    Sleep -> Active:   Ramp stimulation to 100% over 5 seconds

Transitions require the new state to be sustained for at least 5 seconds to prevent false transitions during brief attention lapses.

### Safety Implications

- The [[Stimulation Safety Monitoring System]] must be aware of state transitions to correctly compute duty cycle limits
- Sleep-related neural feature shifts (see [[Neural Feature Stability Across Sleep Cycles]]) affect the accuracy of state classification
- The [[Closed-Loop Grasp Controller]] is paused during non-Active states to prevent unintended prosthetic movement

### Data Logging

State transitions and associated stimulation changes are logged for:
- Session quality analysis
- Sleep architecture documentation (relevant for [[Phase I Trial Design]] secondary outcomes)
- Safety reporting per [[Adverse Event Reporting SOP]]

### Integration with Telemetry

The cortical state is transmitted as metadata alongside neural data via the [[WebSocket Streaming Protocol]] and displayed on the [[Real-Time Telemetry Dashboard]].

#sleep-detection #state-management #safety
`,
	},
	{
		Title:   "Phantom Limb Pain Modulation via Feedback",
		Project: 2,
		Tags:    []string{"phantom-pain", "therapeutic", "neurostimulation"},
		Body: `## Investigating Sensory Feedback for Phantom Limb Pain Modulation

Several participants in our BCI program report phantom limb pain (PLP) in the amputated or deafferented limb. Anecdotal reports suggest that sensory feedback via ICMS may reduce PLP. This note documents our investigation.

### Background

PLP affects 50-80% of amputees and is thought to arise from maladaptive cortical reorganization. By providing structured sensory input through the S1 array, we may reverse some of this reorganization.

### Observational Data

Three participants with chronic PLP have been tracked across sessions. Pain is self-reported on a 0-10 visual analog scale (VAS):

| Participant | Baseline PLP (VAS) | During Feedback (VAS) | 1 hr Post-Session | Duration of Relief |
|-------------|--------------------|-----------------------|--------------------|--------------------|
| P03         | 6.2                | 2.1                   | 3.4                | ~4 hours           |
| P05         | 4.8                | 1.5                   | 2.9                | ~2 hours           |
| P07         | 3.1                | 0.8                   | 1.2                | ~6 hours           |

### Proposed Mechanism

The [[Somatotopic Mapping Procedure]] revealed that stimulation of S1 electrodes mapped to the phantom limb territory evokes natural-feeling percepts in the phantom. This "fills in" the missing sensory input and may normalize cortical representations.

### Stimulation Parameters for Pain Relief

Interestingly, the most effective pain-relief stimulation differs from optimal feedback stimulation:
- Lower frequency (30-50 Hz vs 100+ Hz for feedback)
- Broader spatial activation (more electrodes simultaneously)
- Continuous, not task-linked
- Parameters from [[Micro-stimulation Parameter Space]] are within safe bounds

### Clinical Protocol Considerations

We are preparing an amendment for the [[IRB Protocol Amendments]] to formally include PLP assessment as a secondary outcome in the [[Phase I Trial Design]]. This requires:
- Validated pain assessment tools (Brief Pain Inventory)
- Pre/post session pain diaries
- Safety monitoring for pain exacerbation per [[Adverse Event Reporting SOP]]

### Collaboration

The [[Neurorehab Therapy Suite]] team's [[Motor Recovery Progress Tracking]] system could be adapted to track pain outcomes alongside motor recovery. We are exploring this integration.

### Regulatory Note

If PLP reduction is claimed as a therapeutic benefit, it would change the [[FDA 510k Submission Timeline]] significantly, potentially requiring a de novo classification. The [[EU MDR Classification]] may also be affected. For now, we report it as an incidental observation only.

#phantom-pain #therapeutic #pain-modulation
`,
	},
	{
		Title:   "Phase II Trial Protocol Overview",
		Project: 3,
		Tags:    []string{"clinical-trials", "protocol", "fda"},
		Body: `## Phase II Trial Protocol Overview

This document outlines the Phase II clinical trial protocol for the implantable BCI system, designated as **CT-2026-002**.

### Study Design

- **Type**: Multi-site, single-arm, open-label feasibility study
- **Duration**: 12-month implant period with 24-month follow-up
- **Target enrollment**: 30 participants across 5 sites
- **Primary endpoint**: Successful cursor control within 90 days post-implant

### Inclusion Criteria

1. Adults aged 22-65 with C4-C6 spinal cord injury
2. At least 12 months post-injury with stable neurological status
3. Adequate cortical signal quality confirmed via pre-screening EEG (see [[Consumer EEG Signal Quality]])
4. Willing to attend 3x/week decoder calibration sessions (see [[Decoder Calibration Protocol]])

### Exclusion Criteria

- Active infection or immunosuppression
- Prior cranial surgery within implant target region
- Contraindication to MRI (required for electrode placement planning)
- Participation in another investigational device trial

### Data Collection Schedule

| Visit | Timepoint | Assessments |
|-------|-----------|-------------|
| V1 | Screening | MRI, EEG, neuropsych battery |
| V2 | Baseline | ASIA score, FIM, device fitting |
| V3 | Implant | Surgical procedure, acute telemetry |
| V4-V16 | Weeks 1-12 | Decoder calibration, adverse events |
| V17 | Month 6 | Primary endpoint assessment |
| V18 | Month 12 | Explant decision |

All session data is synchronized via the [[Multi-Site Data Sync Architecture]] to ensure real-time monitoring across sites. Adverse events are tracked per [[Adverse Event Classification Framework]].

#clinical-trials #protocol-design
`,
	},
	{
		Title:   "Adverse Event Classification Framework",
		Project: 3,
		Tags:    []string{"safety", "adverse-events", "clinical-trials"},
		Body: `## Adverse Event Classification Framework

All adverse events (AEs) during the BCI clinical trial must be classified using this standardized framework, aligned with ISO 14155 and FDA 21 CFR 812 requirements.

### Severity Classification

| Grade | Description | Action Required |
|-------|-------------|-----------------|
| 1 | Mild | Documentation only |
| 2 | Moderate | Medical intervention, no hospitalization |
| 3 | Severe | Hospitalization or prolonged stay |
| 4 | Life-threatening | Immediate intervention required |
| 5 | Death | Full root cause analysis |

### Device-Relatedness Assessment

Each AE must be assessed for device relatedness by both the site PI and an independent reviewer:

- **Definitely related**: Direct causal link established
- **Probably related**: Temporal association and plausible mechanism
- **Possibly related**: Temporal association but alternative explanations exist
- **Unlikely related**: No plausible mechanism
- **Unrelated**: Clear alternative etiology

### Common AE Categories for BCI Implants

1. **Surgical**: Infection, hemorrhage, CSF leak
2. **Device**: Lead migration, connector failure, signal degradation
3. **Neurological**: Seizure, headache, cortical spreading depression
4. **Systemic**: Allergic reaction to implant materials (see [[Biocompatibility Test Results Summary]])

### Reporting Timelines

- Serious AEs: Report to sponsor within 24 hours
- Device-related SAEs: FDA MedWatch within 10 business days
- Annual summary: Submitted with [[FDA IDE Annual Report Template]]

All reports must comply with [[HIPAA Compliance Checklist]] to ensure no participant identifiers are included in regulatory submissions. Event data feeds into the [[Real-Time Telemetry Dashboard]] for continuous safety monitoring.

#safety #adverse-events #regulatory
`,
	},
	{
		Title:   "Participant Recruitment Strategy",
		Project: 3,
		Tags:    []string{"recruitment", "clinical-trials", "enrollment"},
		Body: `## Participant Recruitment Strategy

Achieving target enrollment of 30 participants across 5 sites within 6 months requires a coordinated multi-channel recruitment approach.

### Recruitment Channels

1. **SCI rehabilitation centers**: Direct referral partnerships with 12 regional rehab facilities
2. **Patient advocacy groups**: Christopher & Dana Reeve Foundation, United Spinal Association
3. **ClinicalTrials.gov listing**: NCT identifier pending, expected posting week of March 16
4. **Social media campaigns**: Targeted outreach on platforms with SCI community presence
5. **Prior study participants**: Re-contact pool from Phase I (see [[Participant P07 Session Notes]] for exemplar case)

### Site-Level Enrollment Targets

| Site | Institution | Target | PI |
|------|------------|--------|----|
| S01 | Mass General | 8 | Dr. Chen |
| S02 | Johns Hopkins | 7 | Dr. Patel |
| S03 | Stanford | 6 | Dr. Nakamura |
| S04 | Mayo Clinic | 5 | Dr. Olsson |
| S05 | Rancho Los Amigos | 4 | Dr. Reyes |

### Pre-Screening Workflow

Potential participants undergo a two-stage screening process:

1. **Remote pre-screen**: Medical history review, inclusion/exclusion checklist
2. **On-site evaluation**: MRI for cortical mapping, baseline EEG recording, neuropsychological assessment

The EEG pre-screening uses the same hardware platform described in [[Dry Electrode Contact Optimization]] to assess baseline signal quality before committing to invasive recording.

### Retention Strategy

- Monthly participant newsletters with study progress updates
- Travel reimbursement for all study visits
- Dedicated participant coordinator at each site
- 24/7 study hotline for device-related concerns

Enrollment data is tracked per [[Phase II Trial Protocol Overview]] milestones.

#recruitment #enrollment #multi-site
`,
	},
	{
		Title:   "Multi-Site Data Collection Standards",
		Project: 3,
		Tags:    []string{"data-collection", "standardization", "clinical-trials"},
		Body: `## Multi-Site Data Collection Standards

Consistent data collection across all 5 trial sites is critical for regulatory submission and scientific validity. This document defines mandatory standards for all data capture activities.

### Electronic Data Capture (EDC)

All clinical data must be entered into the centralized EDC system within 48 hours of collection. The system enforces:

- Required field validation
- Range checks for physiological parameters
- Automatic audit trail with timestamps and user IDs
- Electronic signatures per 21 CFR Part 11

### Neural Data Standards

Raw neural recordings must adhere to the following specifications:

    Format: BCI2000 .dat with accompanying .prm
    Sampling rate: 30 kHz per channel
    Bit depth: 16-bit signed integer
    Channel mapping: Standard 10-20 extended montage
    File naming: {SiteID}_{ParticipantID}_{SessionDate}_{Run}.dat

All neural data undergoes preprocessing consistent with [[ECoG Signal Preprocessing Pipeline]] before analysis. Data synchronization between sites uses the infrastructure described in [[Multi-Site Data Sync Architecture]].

### Data Quality Metrics

| Metric | Threshold | Frequency |
|--------|-----------|-----------|
| EDC completion rate | > 95% | Weekly |
| Query resolution time | < 5 business days | Ongoing |
| Neural data integrity | < 0.1% packet loss | Per session |
| Signal-to-noise ratio | > 10 dB | Per session |

### Data Privacy

All participant data must be de-identified before transfer between sites. Neural recordings are anonymized per [[Neural Data Anonymization]] procedures. Each site maintains a local key-linking log stored in a separate locked system.

Site monitors conduct quarterly source data verification visits to ensure compliance with [[Phase II Trial Protocol Overview]].

#data-standards #multi-site #quality
`,
	},
	{
		Title:   "Implant Surgery Coordination Checklist",
		Project: 3,
		Tags:    []string{"surgery", "clinical-trials", "checklist"},
		Body: `## Implant Surgery Coordination Checklist

This checklist standardizes the pre-operative, intra-operative, and post-operative workflow for BCI implant procedures across all trial sites.

### Pre-Operative (T-7 to T-1 days)

- [ ] Confirm participant consent form is current (within 30 days)
- [ ] Pre-operative MRI with cortical surface reconstruction complete
- [ ] Electrode array lot number verified against [[Electrode Array Lot Tracking and Traceability]]
- [ ] Surgical team briefing completed with device representative present
- [ ] Telemetry base station tested and firmware current (see [[Implant Firmware OTA Update Protocol]])
- [ ] Anesthesia plan reviewed -- no neuromuscular blockade during cortical mapping phase

### Intra-Operative

- [ ] Craniotomy positioned per MRI-guided neuronavigation plan
- [ ] Electrode array impedance check: all channels < 1 MOhm at 1 kHz
- [ ] Acute neural recording: confirm spiking activity on >= 60% of channels
- [ ] Wireless transmitter link verification (see [[RF Link Budget Analysis]])
- [ ] Connector sealed and pedestal secured with titanium screws
- [ ] Intra-op CT scan to confirm array placement

### Post-Operative (Day 0-7)

- [ ] 24-hour continuous telemetry monitoring
- [ ] Daily impedance trending -- flag any channel with > 50% increase
- [ ] Wound inspection every 12 hours
- [ ] Pain management per site protocol
- [ ] First decoder calibration session scheduled for Day 7 (see [[Decoder Calibration Protocol]])

### Critical Communication Tree

    Event detected -> Site coordinator (< 15 min)
    Site coordinator -> Sponsor medical monitor (< 1 hour)
    Medical monitor -> DSMB chair (< 4 hours for SAEs)

Any surgical complication must be classified per [[Adverse Event Classification Framework]] within 24 hours.

#surgery #checklist #implant
`,
	},
	{
		Title:   "Decoder Training Session Protocol",
		Project: 3,
		Tags:    []string{"decoder", "calibration", "sessions"},
		Body: `## Decoder Training Session Protocol

Post-implant decoder training sessions are the primary intervention in the trial. This protocol standardizes session structure across all sites.

### Session Schedule

- **Weeks 1-4**: 5 sessions/week, 60 minutes each
- **Weeks 5-8**: 3 sessions/week, 90 minutes each
- **Weeks 9-12**: 3 sessions/week, 120 minutes each (including free-use period)

### Standard Session Structure

1. **Impedance check** (5 min): Verify all active channels within specification
2. **Resting state recording** (3 min): Eyes open, eyes closed baseline
3. **Calibration block** (20 min): Center-out cursor task per [[Decoder Calibration Protocol]]
4. **Adaptive block** (20 min): Closed-loop control with real-time decoder updates
5. **Free-use period** (variable): Participant-directed tasks (typing, browsing, art)
6. **Debrief** (10 min): Subjective ratings, fatigue assessment, AE screening

### Performance Metrics

| Metric | Definition | Target (Month 3) |
|--------|-----------|-------------------|
| Acquisition time | Time to reach target | < 2.0 sec |
| Path efficiency | Straight-line / actual path | > 0.7 |
| Bitrate | Information transfer rate | > 2.0 bits/sec |
| Error rate | Failed acquisitions / total | < 15% |

### Decoder Model Updates

The neural decoder uses the architecture described in [[Transformer Decoder Architecture]] with session-specific fine-tuning. Model weights are versioned and stored per [[Training Data Pipeline]] conventions.

Each session generates approximately 2 GB of raw neural data. The preprocessing follows [[ECoG Signal Preprocessing Pipeline]] with site-specific artifact rejection thresholds.

Session videos are recorded for independent review but stored separately from neural data per [[Neural Data Anonymization]] guidelines.

#decoder-training #sessions #performance
`,
	},
	{
		Title:   "Data Safety Monitoring Board Charter",
		Project: 3,
		Tags:    []string{"dsmb", "safety", "oversight"},
		Body: `## Data Safety Monitoring Board Charter

The DSMB provides independent oversight of participant safety and data integrity throughout the Phase II trial.

### Composition

- **Chair**: Independent biostatistician (not affiliated with any trial site)
- **Members**: 2 neurologists, 1 neurosurgeon, 1 bioethicist, 1 patient advocate
- **Non-voting**: Sponsor medical monitor (recused during voting)

### Meeting Schedule

- **Scheduled reviews**: Every 6 months or after every 10th implant (whichever comes first)
- **Ad hoc reviews**: Triggered by any Grade 4-5 adverse event per [[Adverse Event Classification Framework]]

### Stopping Rules

The DSMB may recommend trial suspension or termination if:

1. **Safety boundary**: >= 3 device-related SAEs within any rolling 90-day window
2. **Futility**: < 30% of participants achieve primary endpoint at interim analysis (n=15)
3. **Data integrity**: Evidence of systematic data fabrication or protocol violations

### Data Access

The unblinded statistician prepares reports containing:

- Cumulative enrollment and retention rates
- AE frequency tables by severity and relatedness
- Kaplan-Meier curves for time-to-primary-endpoint
- Device performance metrics (impedance trends, signal quality)

All data reviewed by the DSMB is sourced from the [[Multi-Site Data Sync Architecture]] and cross-validated against source documents during site monitoring visits.

### Recommendations

After each review, the DSMB issues one of:

- **Continue without modification**
- **Continue with protocol amendment** (communicated to [[FDA IDE Annual Report Template]])
- **Temporary hold** pending additional safety data
- **Terminate** the study

Minutes are distributed to all site PIs within 10 business days.

#dsmb #safety #governance
`,
	},
	{
		Title:   "Participant Informed Consent Process",
		Project: 3,
		Tags:    []string{"ethics", "consent", "clinical-trials"},
		Body: `## Participant Informed Consent Process

The informed consent process for an implantable BCI trial requires special attention given the invasive nature of the intervention and the vulnerability of the participant population.

### Consent Document Structure

The ICF (Informed Consent Form) includes the following sections:

1. **Purpose of the study**: Plain-language description of BCI technology and trial goals
2. **Procedures**: Detailed surgical description, session requirements, follow-up schedule
3. **Risks**: Categorized by likelihood and severity
4. **Benefits**: Potential for restored communication/motor control (no guarantee)
5. **Alternatives**: Non-invasive BCI options (see [[Dry Electrode Contact Optimization]] for EEG-based alternatives)
6. **Data usage**: How neural data will be stored, shared, and anonymized per [[Neural Data Anonymization]]
7. **Device explant**: Conditions and procedures for device removal

### Key Risk Disclosures

| Risk | Probability | Mitigation |
|------|-------------|------------|
| Surgical infection | 3-5% | Prophylactic antibiotics, sterile technique |
| Device failure | 5-10% | Redundant wireless link, explant capability |
| Seizure | 1-3% | Anti-epileptic prophylaxis, EEG monitoring |
| Psychological distress | 10-15% | Embedded psychologist, monthly screening |

### Capacity Assessment

All potential participants undergo a standardized capacity assessment:

- MacArthur Competence Assessment Tool for Clinical Research (MacCAT-CR)
- Minimum score of 18/26 required for independent consent
- If capacity is borderline, legally authorized representative must co-sign

### Re-Consent Triggers

Participants must be re-consented if:

- Protocol amendment changes risk profile
- New safety information emerges from [[Data Safety Monitoring Board Charter]] reviews
- Participant requests updated information

The consent process is documented on video for regulatory audit trail. All documents are version-controlled and submitted as part of the [[FDA 510(k) Submission Roadmap]] package.

#consent #ethics #participant-rights
`,
	},
	{
		Title:   "Clinical Outcome Measures Battery",
		Project: 3,
		Tags:    []string{"outcomes", "assessment", "endpoints"},
		Body: `## Clinical Outcome Measures Battery

This document specifies the standardized assessment battery administered at each protocol-defined timepoint during the Phase II trial.

### Primary Outcome Measure

**BCI Performance Assessment (BCI-PA)**

The BCI-PA is a validated 30-minute computer-based assessment measuring:

- 2D cursor control accuracy (center-out task, 8 targets)
- Text entry speed (copy-spelling task, 50 characters)
- Click accuracy (single and double-click on varying target sizes)

Target performance at 6 months: >= 90% cursor accuracy, >= 15 correct chars/min.

### Secondary Outcome Measures

**Functional Independence**

- Functional Independence Measure (FIM): Motor and cognitive subscales
- Spinal Cord Independence Measure (SCIM-III)
- Canadian Occupational Performance Measure (COPM)

**Quality of Life**

- SF-36v2 Health Survey
- Patient Health Questionnaire (PHQ-9) for depression screening
- Psychosocial Impact of Assistive Devices Scale (PIADS)

**Neurological Status**

- ASIA Impairment Scale (baseline and 12-month)
- Modified Ashworth Scale for spasticity

### Assessment Schedule

    Screening:  MRI, EEG, MacCAT-CR, ASIA
    Baseline:   FIM, SCIM-III, SF-36, PHQ-9, COPM
    Month 1:    BCI-PA, PHQ-9
    Month 3:    BCI-PA, FIM, PHQ-9, PIADS
    Month 6:    BCI-PA (primary endpoint), full battery
    Month 12:   Full battery, ASIA

All assessment data is entered into the EDC system per [[Multi-Site Data Collection Standards]]. Assessors must be blinded to decoder performance metrics to avoid bias. Inter-rater reliability checks are conducted quarterly.

Results feed into the adaptive difficulty system for therapy applications (see [[Adaptive Difficulty Algorithm]]) and inform decoder optimization per [[Decoder Training Session Protocol]].

#outcomes #assessment #endpoints
`,
	},
	{
		Title:   "Site Monitoring and Audit Plan",
		Project: 3,
		Tags:    []string{"monitoring", "quality-assurance", "compliance"},
		Body: `## Site Monitoring and Audit Plan

Ongoing monitoring ensures data integrity, participant safety, and regulatory compliance across all 5 trial sites.

### Monitoring Visit Schedule

| Visit Type | Frequency | Duration | Focus |
|-----------|-----------|----------|-------|
| Initiation | Once per site | 2 days | Training, system setup, delegation log |
| Routine | Every 8 weeks | 1-2 days | SDV, consent review, AE reconciliation |
| For-cause | As needed | Variable | Protocol deviations, safety signals |
| Close-out | End of enrollment | 2 days | Final data lock, document archival |

### Source Data Verification (SDV)

- 100% SDV for primary endpoint data
- 100% SDV for all serious adverse events
- 20% random sample SDV for secondary endpoints
- 100% verification of informed consent documents

### Critical Monitoring Activities

1. **Consent audit**: Verify consent date precedes any study procedure
2. **Eligibility confirmation**: Cross-check inclusion/exclusion against source documents
3. **Device accountability**: Reconcile electrode array serial numbers with [[Electrode Array Lot Tracking and Traceability]]
4. **Data query resolution**: Ensure all open queries resolved within 5 business days
5. **Protocol deviation log**: Review and classify all deviations

### Risk-Based Monitoring

In addition to on-site visits, centralized statistical monitoring is performed weekly:

- Key risk indicators (KRIs) tracked via [[Real-Time Telemetry Dashboard]]
- Enrollment rate deviations flagged if > 20% behind target
- Data entry lag alerts for EDC entries > 48 hours post-visit
- AE reporting compliance monitored per [[Adverse Event Classification Framework]]

### Audit Readiness

All sites must maintain an inspection-ready Trial Master File (TMF) at all times. The TMF structure follows ICH E6(R2) and aligns with the document requirements in [[ISO 14155 Compliance Matrix]].

#monitoring #sdv #quality
`,
	},
	{
		Title:   "Long-Term Follow-Up Protocol",
		Project: 3,
		Tags:    []string{"follow-up", "long-term", "safety"},
		Body: `## Long-Term Follow-Up Protocol

Participants who complete the 12-month active trial period enter the long-term follow-up (LTFU) phase, which continues for an additional 4 years post-implant.

### Rationale

The FDA requires long-term safety data for permanently implanted neural devices. Key concerns include:

- Chronic electrode-tissue interface stability
- Long-term biocompatibility of array materials (see [[Biocompatibility Test Results Summary]])
- Psychological adaptation and dependency
- Device degradation and failure modes

### LTFU Visit Schedule

| Timepoint | Visit Type | Assessments |
|-----------|-----------|-------------|
| Month 18 | In-person | Impedance, imaging, BCI-PA, AE review |
| Month 24 | In-person | Full assessment battery, CT scan |
| Year 3 | Telehealth | AE screening, device status, PHQ-9 |
| Year 4 | Telehealth | AE screening, device status |
| Year 5 | In-person | Final comprehensive evaluation |

### Device Monitoring During LTFU

Even after active trial participation ends, participants retain the implanted device. Ongoing monitoring includes:

- Monthly automated impedance checks via wireless telemetry
- Quarterly data uploads per [[Implant Firmware OTA Update Protocol]] maintenance cycle
- Annual RF link integrity verification per [[RF Link Budget Analysis]]

### Explant Criteria

Device removal is offered if:

1. Participant requests explant for any reason
2. Device-related SAE that cannot be managed conservatively
3. Complete signal loss (> 90% of channels non-functional for > 30 days)
4. End of LTFU period (participant choice to keep or remove)

### Data Continuity

LTFU data is collected in the same EDC system used during the active trial. The [[Multi-Site Data Sync Architecture]] maintains data availability even if individual site systems are updated. Progress is reported through [[Motor Recovery Progress Tracking]] for participants who continue therapy.

#follow-up #long-term #post-market
`,
	},
	{
		Title:   "Closed-Loop Therapy Session Design",
		Project: 3,
		Tags:    []string{"therapy", "closed-loop", "rehabilitation"},
		Body: `## Closed-Loop Therapy Session Design

This document describes the design of closed-loop therapy sessions that integrate BCI control with functional electrical stimulation (FES) for motor rehabilitation.

### Session Architecture

The closed-loop system connects three components:

1. **Neural decoder**: Extracts motor intent from implanted electrode array
2. **FES controller**: Delivers stimulation to paralyzed muscles based on decoded intent
3. **Sensory feedback**: Provides proprioceptive feedback via cortical micro-stimulation (see [[Micro-stimulation Parameter Space]])

### Control Flow

    Neural recording -> Decode motor intent (50ms latency budget)
    Motor intent -> FES parameter mapping (10ms)
    FES delivery -> Muscle contraction
    Force sensor -> Sensory encoding -> Cortical stimulation (20ms)

Total loop latency must remain < 100ms to maintain perceptual continuity per [[Closed-Loop Grasp Controller]] specifications.

### Therapy Protocol Levels

| Level | Task | Success Criterion |
|-------|------|-------------------|
| 1 | Wrist extension | Sustained 3s hold |
| 2 | Grip open/close | 5 reps in 30s |
| 3 | Object transfer | Cup lift and place |
| 4 | Bimanual task | Jar open with two hands |
| 5 | Functional ADL | Self-feeding task |

Difficulty progression follows the algorithm described in [[Adaptive Difficulty Algorithm]], adapted for FES-specific parameters.

### Neural Plasticity Monitoring

We track cortical map reorganization across sessions by:

- Comparing tuning curves from [[Decoder Training Session Protocol]] calibration blocks
- Monitoring beta-band desynchronization patterns
- Quantifying representational similarity across weeks

### Safety Interlocks

- Maximum FES current: 25 mA per channel
- Emergency stop accessible to both participant and therapist
- Automatic shutoff if decoded intent confidence < 60%
- All sessions recorded per [[Multi-Site Data Collection Standards]]

#therapy #closed-loop #fes #rehabilitation
`,
	},
	{
		Title:   "Pediatric BCI Trial Feasibility Assessment",
		Project: 3,
		Tags:    []string{"pediatric", "feasibility", "clinical-trials"},
		Body: `## Pediatric BCI Trial Feasibility Assessment

This document evaluates the feasibility of extending the BCI clinical trial program to pediatric participants (ages 12-17) with acquired brain injury.

### Clinical Need

Pediatric patients with locked-in syndrome or severe motor impairment from brainstem stroke or traumatic brain injury represent an underserved population. Current assistive technologies (eye tracking, switch scanning) often fail to meet the communication needs of developing adolescents.

### Regulatory Pathway

Pediatric device trials require additional regulatory considerations:

- FDA Pediatric Device Consortia consultation recommended
- Humanitarian Device Exemption (HDE) may apply for populations < 8,000/year
- Additional IRB scrutiny per [[ISO 14155 Compliance Matrix]] pediatric addendum
- Assent process required in addition to parental consent

### Technical Considerations

| Factor | Adult Trial | Pediatric Adaptation |
|--------|-----------|---------------------|
| Skull thickness | 6-8 mm | 3-5 mm (modified array needed) |
| Cranial growth | Stable | Active through age 18 |
| Electrode size | 4x4 mm array | 3x3 mm array variant (see [[Electrode Array Design Specifications]]) |
| Session duration | 60-120 min | 30-60 min max |
| Decoder model | Transfer learning | Age-specific training via [[Training Data Pipeline]] |

### Ethical Considerations

- Capacity for assent varies with developmental stage
- Long-term device commitment during formative years
- Impact on school attendance and social development
- Parental decision-making burden

### Recommended Next Steps

1. Convene pediatric neurology advisory panel
2. Develop age-appropriate outcome measures
3. Design modified electrode array for thinner pediatric skull
4. Submit pre-IDE meeting request to FDA per [[FDA IDE Annual Report Template]] framework
5. Engage pediatric patient advocacy organizations

This assessment should be reviewed alongside [[Participant Recruitment Strategy]] to determine whether a separate pediatric trial or an age-expansion amendment is more appropriate.

#pediatric #feasibility #special-populations
`,
	},
	{
		Title:   "Neuropsychological Screening Battery",
		Project: 3,
		Tags:    []string{"neuropsych", "screening", "assessment"},
		Body: `## Neuropsychological Screening Battery

All trial participants undergo a standardized neuropsychological screening to establish baseline cognitive function and monitor for device-related cognitive changes.

### Pre-Implant Battery

The following assessments are administered by a licensed neuropsychologist at screening:

1. **Cognitive screening**: Montreal Cognitive Assessment (MoCA) -- minimum score 22/30
2. **Attention**: Trail Making Test Parts A & B
3. **Memory**: Rey Auditory Verbal Learning Test (RAVLT)
4. **Executive function**: Wisconsin Card Sorting Test (WCST)
5. **Language**: Boston Naming Test (adapted for BCI response modality)
6. **Visuospatial**: Judgment of Line Orientation

### Adaptation for Motor-Impaired Participants

Standard neuropsych tests assume motor response capability. Adaptations include:

- Eye-tracking response interface for timed tests
- Extended time limits (2x standard) for motor-dependent subtests
- BCI-compatible response paradigms post-implant (see [[Decoder Training Session Protocol]])
- Verbal response capture via caregiver transcription

### Monitoring Schedule

    Pre-implant:  Full battery (2-3 hours)
    Month 3:      MoCA, TMT, PHQ-9 (45 min)
    Month 6:      Full battery (primary endpoint)
    Month 12:     Full battery (study exit)
    Annually:     MoCA, PHQ-9 (LTFU per [[Long-Term Follow-Up Protocol]])

### Red Flag Criteria

Immediate referral to the site PI and DSMB is triggered by:

- MoCA decline >= 4 points from baseline
- New-onset aphasia not explained by pre-existing condition
- PHQ-9 score >= 15 (moderately severe depression)
- Participant or caregiver report of personality change

These triggers feed into the safety monitoring framework defined in [[Data Safety Monitoring Board Charter]]. Cognitive data is de-identified before multi-site aggregation per [[HIPAA Compliance Checklist]].

#neuropsych #cognitive #screening #safety
`,
	},
	{
		Title:   "FDA IDE Annual Report Template",
		Project: 4,
		Tags:    []string{"fda", "ide", "regulatory"},
		Body: `## FDA IDE Annual Report Template

This template structures the annual progress report required under 21 CFR 812.150(b)(5) for the investigational device exemption covering the implantable BCI system.

### Required Sections

1. **Investigational Plan Summary**: Current protocol version, approved amendments
2. **Subject Enrollment**: Cumulative enrollment vs. target, by site
3. **Subject Disposition**: Withdrawals, completions, screen failures
4. **Adverse Events**: Tabulated by type, severity, and device relatedness per [[Adverse Event Classification Framework]]
5. **Protocol Deviations**: Classified as major or minor, corrective actions taken
6. **Device Modifications**: Any changes to the electrode array or transmitter (see [[Wireless Transmitter Power Budget]])
7. **Investigator Changes**: New PIs, delegation log updates

### Safety Summary Table

| Category | Total | Device-Related | Serious |
|----------|-------|---------------|---------|
| Surgical complications | -- | -- | -- |
| Device malfunctions | -- | -- | -- |
| Neurological events | -- | -- | -- |
| Infections | -- | -- | -- |
| Other | -- | -- | -- |

### Manufacturing Updates

Any changes to the electrode array manufacturing process must be reported, including:

- New raw material suppliers
- Process parameter changes (see [[Cleanroom Process Parameter Optimization]])
- Yield data and failure analysis summaries
- Biocompatibility testing updates per [[Biocompatibility Test Results Summary]]

### Submission Logistics

- Due date: Within 3 months of IDE anniversary
- Submit via FDA ESG (Electronic Submissions Gateway)
- Include cover letter referencing IDE number G260XXX
- Cross-reference any supplemental submissions from the year

All data in this report must be reconciled with [[Multi-Site Data Collection Standards]] before submission. Statistical summaries are prepared by the unblinded biostatistician per [[Data Safety Monitoring Board Charter]] procedures.

#fda #ide #annual-report
`,
	},
	{
		Title:   "FDA 510(k) Submission Roadmap",
		Project: 4,
		Tags:    []string{"fda", "510k", "premarket"},
		Body: `## FDA 510(k) Submission Roadmap

This document outlines the regulatory pathway for obtaining 510(k) clearance for the non-invasive EEG-based BCI accessory system, which serves as a companion screening tool for the implantable device.

### Predicate Device Analysis

| Feature | Our Device | Predicate (K192XXX) |
|---------|-----------|-------------------|
| Modality | EEG (32-channel) | EEG (64-channel) |
| Indication | BCI screening | Cognitive assessment |
| Contact type | Dry electrode | Wet electrode |
| Output | Signal quality score | Raw EEG data |
| Classification | Class II | Class II |

The dry electrode technology leverages advances described in [[Dry Electrode Contact Optimization]].

### Submission Timeline

    Month 1-2:  Pre-submission (Q-Sub) meeting with FDA
    Month 3-4:  Bench testing and biocompatibility
    Month 5-6:  Software validation (IEC 62304)
    Month 7-8:  Clinical data analysis
    Month 9:    Submission preparation
    Month 10:   Submit 510(k)
    Month 13:   Expected clearance (90-day review)

### Key Submission Components

1. **Device description**: Technical specifications, system architecture
2. **Substantial equivalence**: Comparison table with predicate
3. **Performance testing**: Signal quality validation per [[Consumer EEG Signal Quality]] benchmarks
4. **Software documentation**: Per IEC 62304, including [[SDK Architecture Overview]] for API components
5. **Biocompatibility**: ISO 10993 testing for skin-contact electrodes
6. **Labeling**: Draft IFU, warnings, contraindications
7. **Clinical evidence**: Literature review + trial screening data from [[Phase II Trial Protocol Overview]]

### Risk Classification

This device falls under 21 CFR 882.1400 (Electroencephalograph) and [[Risk Management File Structure]] must demonstrate conformity with ISO 14971.

#510k #fda #premarket #clearance
`,
	},
	{
		Title:   "EU MDR Classification and Strategy",
		Project: 4,
		Tags:    []string{"eu-mdr", "ce-marking", "regulatory"},
		Body: `## EU MDR Classification and Strategy

The implantable BCI system is classified under EU MDR 2017/745 and requires a comprehensive conformity assessment before CE marking.

### Device Classification

Under Annex VIII classification rules:

- **Rule 8** (invasive devices in contact with the central nervous system): **Class III**
- Requires Notified Body involvement for conformity assessment
- Clinical investigation required under Article 62

### Conformity Assessment Route

We will pursue Annex IX (Quality Management System) + Annex X (Type Examination):

1. **QMS certification**: Based on [[ISO 13485 QMS Gap Analysis]] -- our existing QMS covers most requirements
2. **Technical documentation**: Per Annex II, including clinical evaluation report
3. **Type examination**: Notified Body reviews complete design dossier
4. **Clinical evaluation**: MEDDEV 2.7/1 Rev 4 clinical evaluation report

### Key Differences from FDA Pathway

| Aspect | FDA (IDE/PMA) | EU MDR |
|--------|-------------|--------|
| Clinical data | IDE trial data | Clinical investigation per Article 62 |
| QMS | Not required for approval | Mandatory ISO 13485 |
| Post-market | Annual reports | PMCF plan + periodic safety updates |
| Unique ID | N/A | UDI-DI per EUDAMED |
| Authorized rep | N/A | Required (Article 11) |

### EUDAMED Registration

Before placing the device on the EU market:

- Register as manufacturer in EUDAMED Actor Registration module
- Submit UDI-DI data per [[Device Labeling and UDI Requirements]]
- Upload Summary of Safety and Clinical Performance (SSCP)
- Register clinical investigations

### Timeline

Estimated 18-24 months from Notified Body engagement to CE mark. The Notified Body audit will review manufacturing processes including those at [[Cleanroom Process Parameter Optimization]] and design controls per [[Design History File Organization]].

#eu-mdr #ce-marking #class-iii
`,
	},
	{
		Title:   "ISO 13485 QMS Gap Analysis",
		Project: 4,
		Tags:    []string{"iso-13485", "qms", "quality"},
		Body: `## ISO 13485 QMS Gap Analysis

This analysis identifies gaps between our current quality management system and the requirements of ISO 13485:2016 for medical device manufacturers.

### Assessment Summary

| Clause | Requirement | Status | Gap |
|--------|-----------|--------|-----|
| 4.1 | QMS general requirements | Partial | Process interaction map incomplete |
| 4.2 | Documentation requirements | Partial | DHF structure needs formalization |
| 5.1 | Management commitment | Complete | -- |
| 5.6 | Management review | Gap | No formal review schedule |
| 6.2 | Human resources | Partial | Training matrix incomplete |
| 6.4 | Work environment | Complete | Cleanroom qualified |
| 7.1 | Product realization planning | Partial | Risk mgmt not integrated |
| 7.3 | Design and development | Partial | See [[Design History File Organization]] |
| 7.4 | Purchasing | Gap | Supplier qualification process needed |
| 7.5 | Production and service | Partial | Process validation incomplete |
| 8.2 | Monitoring and measurement | Gap | Internal audit program not established |
| 8.5 | Improvement | Gap | CAPA process not formalized |

### Priority Actions

1. **CAPA system** (Critical): Implement corrective and preventive action procedures. Target: 4 weeks.
2. **Supplier qualification**: Establish approved supplier list with qualification criteria for electrode array materials.
3. **Internal audit program**: Schedule quarterly audits covering all QMS clauses over 12-month cycle.
4. **Design controls**: Formalize design input/output/verification/validation linkage per [[Design History File Organization]].
5. **Document control**: Migrate from ad-hoc versioning to compliant document control system.

### Impact on Regulatory Submissions

ISO 13485 certification is mandatory for [[EU MDR Classification and Strategy]] and strengthens the quality system section of any FDA PMA submission. The gap analysis feeds directly into our [[Risk Management File Structure]] per ISO 14971.

Manufacturing processes described in [[Cleanroom Process Parameter Optimization]] require validated procedures under clause 7.5.6 (validation of processes for production).

#iso-13485 #qms #gap-analysis
`,
	},
	{
		Title:   "Risk Management File Structure",
		Project: 4,
		Tags:    []string{"risk-management", "iso-14971", "safety"},
		Body: `## Risk Management File Structure

The risk management file for the implantable BCI system follows ISO 14971:2019 and is maintained as a living document throughout the device lifecycle.

### File Organization

    /risk-management/
        risk-management-plan.md
        hazard-identification/
            electrical-hazards.md
            biological-hazards.md
            mechanical-hazards.md
            software-hazards.md
            use-error-hazards.md
        risk-analysis/
            fmea-electrode-array.md
            fmea-wireless-transmitter.md
            fmea-decoder-software.md
            fault-tree-analysis.md
        risk-evaluation/
            risk-matrix.md
            residual-risk-assessment.md
        risk-control/
            design-mitigations.md
            labeling-mitigations.md
            training-mitigations.md
        risk-management-report.md

### Risk Acceptability Matrix

| Severity | Negligible | Minor | Serious | Critical | Catastrophic |
|----------|-----------|-------|---------|----------|-------------|
| Frequent | M | H | H | U | U |
| Probable | L | M | H | U | U |
| Occasional | L | M | H | H | U |
| Remote | L | L | M | H | H |
| Improbable | L | L | L | M | H |

L = Acceptable, M = ALARP review, H = Risk reduction required, U = Unacceptable

### Top Hazards Identified

1. **Electrode array fracture during implant**: Mitigated by mechanical testing per [[Electrode Impedance Spectroscopy Standards]]
2. **Wireless link failure during therapy**: Mitigated by safe-state fallback per [[RF Link Budget Analysis]]
3. **Decoder misclassification of intent**: Mitigated by confidence thresholds per [[Low-Latency Intent Classification]]
4. **Infection at implant site**: Mitigated by biocompatible materials per [[Biocompatibility Test Results Summary]]

### Regulatory Cross-References

This file supports submissions for both [[FDA 510(k) Submission Roadmap]] and [[EU MDR Classification and Strategy]]. The FMEA for the wireless transmitter references design specifications from [[Wireless Transmitter Power Budget]].

#risk-management #iso-14971 #hazard-analysis
`,
	},
	{
		Title:   "ISO 14155 Compliance Matrix",
		Project: 4,
		Tags:    []string{"iso-14155", "clinical-investigation", "compliance"},
		Body: `## ISO 14155 Compliance Matrix

This matrix maps ISO 14155:2020 requirements for clinical investigations of medical devices to our trial documentation and processes.

### Compliance Status

| Clause | Requirement | Evidence Document | Status |
|--------|-----------|-------------------|--------|
| 5.1 | Ethical considerations | IRB approvals, consent forms | Complete |
| 5.2 | Risk-benefit assessment | [[Risk Management File Structure]] | Complete |
| 6.1 | Clinical investigation plan | [[Phase II Trial Protocol Overview]] | Complete |
| 6.2 | Selection of investigators | Site qualification reports | Complete |
| 7.1 | CRF design | EDC specification document | Complete |
| 7.2 | Data management | [[Multi-Site Data Collection Standards]] | Complete |
| 8.1 | AE reporting | [[Adverse Event Classification Framework]] | Complete |
| 8.2 | Device deficiency reporting | Device malfunction SOP | In Progress |
| 9.1 | Monitoring | [[Site Monitoring and Audit Plan]] | Complete |
| 10.1 | Statistical analysis plan | SAP v2.1 | Complete |
| 11.1 | Clinical investigation report | Template drafted | Pending |

### Gap Remediation

Two areas require immediate attention:

**Device Deficiency Reporting (Clause 8.2)**

A formal procedure for documenting and reporting device deficiencies is needed. This must cover:

- Electrode array signal degradation events
- Wireless transmitter communication failures per [[Wireless Transmitter Power Budget]] specifications
- Firmware anomalies per [[Implant Firmware OTA Update Protocol]]

**Clinical Investigation Report (Clause 11.1)**

The CIR template must be finalized before the first participant completes the 12-month visit. Structure follows:

1. Participant demographics and disposition
2. Primary and secondary endpoint analysis
3. Safety summary with device-relatedness assessment
4. Risk-benefit conclusion

This compliance matrix is reviewed quarterly and updated as documentation matures. It feeds into both [[FDA IDE Annual Report Template]] and [[EU MDR Classification and Strategy]] submissions.

#iso-14155 #compliance #clinical-investigation
`,
	},
	{
		Title:   "Design History File Organization",
		Project: 4,
		Tags:    []string{"design-controls", "dhf", "quality"},
		Body: `## Design History File Organization

The Design History File (DHF) is the complete record of design controls applied throughout the development of the implantable BCI system, as required by 21 CFR 820.30 and ISO 13485 clause 7.3.

### DHF Structure

    /design-history/
        design-plan/
            design-development-plan.md
            project-schedule.md
        design-input/
            user-needs.md
            design-requirements.md
            regulatory-requirements.md
        design-output/
            system-architecture.md
            electrode-array-specifications.md
            software-requirements-spec.md
            wireless-subsystem-spec.md
        verification/
            bench-test-protocols.md
            bench-test-reports.md
            software-test-reports.md
        validation/
            clinical-investigation-plan.md
            usability-study-report.md
        design-review/
            review-minutes-phase-1.md
            review-minutes-phase-2.md
            review-minutes-phase-3.md
        design-transfer/
            manufacturing-process-spec.md
            acceptance-criteria.md

### Traceability Matrix

Every requirement must be traceable from user need through design input, design output, verification, and validation:

    User Need -> Design Input -> Design Output -> Verification -> Validation

For example:

- UN-001: User needs real-time cursor control
- DI-001: Decoder latency < 50ms
- DO-001: [[Transformer Decoder Architecture]] with streaming inference
- VER-001: Bench test with simulated neural data
- VAL-001: Clinical trial cursor task per [[Clinical Outcome Measures Battery]]

### Design Reviews

Formal design reviews are conducted at each phase gate. Minimum attendees:

- Project lead, systems engineer, quality engineer, regulatory specialist
- Independent reviewer not on the design team

All design changes after transfer to manufacturing must follow the change control process and may trigger updates to [[Electrode Array Design Specifications]] and [[Risk Management File Structure]].

#design-controls #dhf #traceability
`,
	},
	{
		Title:   "Device Labeling and UDI Requirements",
		Project: 4,
		Tags:    []string{"labeling", "udi", "regulatory"},
		Body: `## Device Labeling and UDI Requirements

This document specifies labeling requirements for the implantable BCI system under FDA 21 CFR 801 and EU MDR Annex I Chapter III.

### UDI Structure

The Unique Device Identifier follows GS1 standards:

    UDI-DI (Device Identifier): (01)00860009XXXXXX
    UDI-PI (Production Identifier): (10){LotNumber}(17){ExpirationDate}(21){SerialNumber}

Each electrode array carries a unique UDI encoded as:

- Human-readable text on the sterile packaging
- GS1 DataMatrix barcode (minimum X-dimension 0.254 mm)
- RFID tag embedded in the shipping container

### Labeling Content Requirements

**Sterile Package Label**

| Element | FDA Requirement | EU MDR Requirement |
|---------|---------------|-------------------|
| Device name | Required | Required |
| UDI barcode | Required | Required |
| Manufacturer | Name + address | Name + address |
| Lot/Serial | Required | Required |
| Sterility | Method + expiration | Method + expiration |
| Warnings | "Rx only" | CE mark + NB number |
| Storage conditions | Required | Required |

**Instructions for Use (IFU)**

The IFU must include surgical implantation instructions, decoder setup procedures, and troubleshooting guidance. The decoder setup references [[SDK Architecture Overview]] for API integration and [[REST API Specification]] for programmatic configuration.

### Traceability Integration

UDI data links to the manufacturing records in [[Electrode Array Lot Tracking and Traceability]]. Each array's lot number maps to:

- Raw material certificates of analysis
- Cleanroom batch records
- Biocompatibility test certificates per [[Biocompatibility Test Results Summary]]
- Sterilization records

### EUDAMED Submission

For EU market access, UDI data is submitted to EUDAMED per [[EU MDR Classification and Strategy]]. The Summary of Safety and Clinical Performance (SSCP) is publicly accessible through the EUDAMED database.

#labeling #udi #packaging
`,
	},
	{
		Title:   "Post-Market Surveillance Plan",
		Project: 4,
		Tags:    []string{"post-market", "surveillance", "pms"},
		Body: `## Post-Market Surveillance Plan

This plan defines our systematic approach to collecting, analyzing, and acting on post-market data for the implantable BCI system, as required by EU MDR Article 83 and FDA 21 CFR 822.

### Data Sources

1. **Customer complaints**: Structured intake via quality management system
2. **Clinical trial LTFU data**: Ongoing per [[Long-Term Follow-Up Protocol]]
3. **Literature monitoring**: Monthly systematic review of BCI-related publications
4. **Adverse event databases**: FDA MAUDE, EU Eudravigilance, WHO ICSR
5. **Field service reports**: From on-site technical support visits
6. **Implant registry**: National registry participation where required

### Surveillance Activities

| Activity | Frequency | Responsible |
|----------|-----------|-------------|
| Complaint trend analysis | Monthly | Quality Manager |
| Literature review | Monthly | Regulatory Affairs |
| Registry data review | Quarterly | Clinical Affairs |
| Proactive field monitoring | Continuous | Field Service |
| PSUR preparation | Annual | Regulatory Affairs |

### Periodic Safety Update Report (PSUR)

Annual PSURs include:

- Cumulative implant count and exposure
- Complaint and AE summary with trending
- Risk-benefit re-evaluation per [[Risk Management File Structure]]
- Comparison with state-of-the-art
- Conclusions and recommended actions

### Post-Market Clinical Follow-Up (PMCF)

The PMCF plan specifies:

- Continued data collection from [[Phase II Trial Protocol Overview]] participants in LTFU
- Registry-based outcomes tracking
- Targeted literature review on electrode-tissue interface longevity
- Proactive survey of implanting surgeons

### Signal Detection

Statistical process control methods are applied to key indicators:

- Impedance drift rate exceeding 3-sigma from baseline
- Infection rate > 2x published benchmarks
- Decoder performance degradation correlated with implant age

Signals trigger investigation and potential corrective action per the CAPA process defined in [[ISO 13485 QMS Gap Analysis]].

#post-market #surveillance #pmcf
`,
	},
	{
		Title:   "Software as Medical Device (SaMD) Classification",
		Project: 4,
		Tags:    []string{"samd", "software", "regulatory"},
		Body: `## Software as Medical Device (SaMD) Classification

The BCI system includes multiple software components that may qualify as Software as a Medical Device under IMDRF and FDA guidance.

### Software Component Classification

| Component | Function | SaMD? | Risk Category |
|-----------|----------|-------|---------------|
| Neural decoder | Translates brain signals to commands | Yes | Class III (high risk) |
| Calibration software | Tunes decoder parameters | Yes | Class II |
| Telemetry dashboard | Displays device status | Yes | Class I |
| Patient app | Session scheduling, diary | No | N/A (wellness) |
| Cloud sync platform | Data aggregation | Possible | Pending review |

### FDA Framework for SaMD

Using the IMDRF categorization framework:

- **Significance of information**: Drives clinical management (neural decoder) vs. informs (dashboard)
- **Healthcare situation**: Critical (implanted device control) vs. non-serious (scheduling)

The neural decoder falls into Category IV (treat or diagnose, critical situation) and requires the highest level of clinical evidence.

### IEC 62304 Software Lifecycle

All SaMD components must follow IEC 62304:

    Class A: No injury possible -> Dashboard
    Class B: Non-serious injury possible -> Calibration SW
    Class C: Death or serious injury possible -> Neural decoder

The decoder software architecture is documented in [[Transformer Decoder Architecture]] and uses the API layer described in [[Sensory Feedback Integration API]] for closed-loop applications.

### Cybersecurity Requirements

Per FDA premarket cybersecurity guidance (2023):

- Threat modeling for all networked components
- Software Bill of Materials (SBOM) maintained
- Patch management plan per [[Implant Firmware OTA Update Protocol]]
- Penetration testing prior to submission

All software validation evidence is maintained in the [[Design History File Organization]] and supports both [[FDA 510(k) Submission Roadmap]] and [[EU MDR Classification and Strategy]] submissions.

#samd #iec-62304 #software-classification
`,
	},
	{
		Title:   "Regulatory Submission Document Control",
		Project: 4,
		Tags:    []string{"document-control", "submissions", "quality"},
		Body: `## Regulatory Submission Document Control

This procedure governs the creation, review, approval, and archival of all documents included in regulatory submissions.

### Document Numbering Convention

    {Category}-{Sequence}-{Version}

    Categories:
    REG - Regulatory strategy and correspondence
    CER - Clinical evaluation reports
    DHF - Design history file documents
    RMF - Risk management file documents
    QMS - Quality management system documents
    LBL - Labeling and IFU documents
    MFG - Manufacturing and process documents

Example: REG-042-v03 = Regulatory document #42, version 3

### Review and Approval Workflow

| Document Type | Author | Reviewer | Approver |
|--------------|--------|----------|----------|
| Regulatory strategy | RA Manager | VP Regulatory | CEO |
| Clinical protocols | Clinical Director | RA Manager, Medical Monitor | VP Clinical |
| Risk documents | Quality Engineer | RA Manager | VP Quality |
| Design documents | Systems Engineer | Quality Engineer | VP Engineering |
| Manufacturing SOPs | Process Engineer | Quality Engineer | VP Manufacturing |

### Version Control Rules

- Draft documents use v0.X numbering
- Approved documents use integer versions (v1, v2, etc.)
- Once approved, changes require a formal change request
- Superseded versions are archived, not deleted
- All changes tracked with rationale

### Submission Assembly

Before any regulatory submission, the RA Manager assembles the package:

1. Verify all component documents are at approved versions
2. Cross-check references (e.g., [[Risk Management File Structure]] version matches what is cited in clinical evaluation)
3. Generate Table of Contents with document version numbers
4. Export as eCTD format for FDA or STED format for EU
5. Final QA review against [[ISO 13485 QMS Gap Analysis]] document control requirements

### Archival

All submitted packages are archived with:

- Submission date, regulatory body, reference number
- Complete copy of submitted documents (PDF/A format)
- Acknowledgment receipt from regulatory authority
- Link to any related correspondence in [[FDA IDE Annual Report Template]]

#document-control #version-management #submissions
`,
	},
	{
		Title:   "Clinical Evidence Strategy",
		Project: 4,
		Tags:    []string{"clinical-evidence", "regulatory", "strategy"},
		Body: `## Clinical Evidence Strategy

This document outlines the overall strategy for generating and presenting clinical evidence to support regulatory submissions in the US, EU, and other target markets.

### Evidence Hierarchy

Our clinical evidence package combines multiple sources:

1. **Pivotal clinical trial**: Phase II data from [[Phase II Trial Protocol Overview]] (primary evidence)
2. **Literature review**: Systematic review of published BCI clinical data
3. **Predicate/equivalent device data**: For 510(k) pathway per [[FDA 510(k) Submission Roadmap]]
4. **Bench testing**: Performance data correlated with clinical outcomes
5. **Post-market data**: Ongoing collection per [[Post-Market Surveillance Plan]]

### Evidence Requirements by Market

| Market | Regulatory Body | Evidence Standard |
|--------|---------------|-------------------|
| US (PMA) | FDA | Valid scientific evidence, pivotal trial |
| US (510k) | FDA | Substantial equivalence + performance |
| EU | Notified Body | Sufficient clinical evidence per MDR Art. 61 |
| Japan | PMDA | Japanese clinical data may be required |
| Australia | TGA | Accepts FDA/CE data with local review |

### Clinical Evaluation Report (CER)

The CER is the central document for EU MDR compliance:

- Follows MEDDEV 2.7/1 Rev 4 methodology
- Systematic literature search protocol with defined databases and keywords
- Appraisal of each data source for relevance and quality
- Clinical data analysis including safety and performance
- Benefit-risk determination referencing [[Risk Management File Structure]]

### Gap Analysis

| Evidence Type | Available | Gap |
|--------------|-----------|-----|
| Bench performance data | Yes | None |
| Biocompatibility (ISO 10993) | Yes | See [[Biocompatibility Test Results Summary]] |
| Electrical safety (IEC 60601) | In progress | Wireless EMC testing pending |
| Usability (IEC 62366) | In progress | Formative study complete, summative pending |
| Clinical trial data | Collecting | Per [[Multi-Site Data Collection Standards]] |
| PMCF data | Not yet | Collection begins post-approval |

The clinical evidence strategy is reviewed semi-annually and updated as new data becomes available.

#clinical-evidence #strategy #regulatory
`,
	},
	{
		Title:   "Electrode Array Design Specifications",
		Project: 5,
		Tags:    []string{"electrode-array", "design", "specifications"},
		Body: `## Electrode Array Design Specifications

This document defines the design specifications for the implantable micro-electrode array used in the BCI system.

### Physical Specifications

| Parameter | Value | Tolerance |
|-----------|-------|-----------|
| Array dimensions | 4.0 x 4.0 mm | +/- 0.1 mm |
| Electrode count | 96 (10x10 minus corners) | N/A |
| Electrode pitch | 400 um | +/- 10 um |
| Electrode length | 1.5 mm (signal), 0.5 mm (reference) | +/- 0.05 mm |
| Shaft diameter | 80 um at base, tapered | +/- 5 um |
| Tip radius | < 5 um | Max 5 um |
| Base material | Silicon (single crystal, <100>) | N/A |
| Tip metallization | Sputtered iridium oxide (SIROF) | 200 +/- 20 nm |
| Insulation | Parylene-C | 5 +/- 0.5 um |

### Electrical Requirements

- Impedance at 1 kHz: 100-800 kOhm per electrode
- Charge storage capacity: > 3 mC/cm2
- Channel-to-channel crosstalk: < -40 dB
- Noise floor: < 5 uVrms (10 Hz - 10 kHz bandwidth)

These impedance targets are verified per [[Electrode Impedance Spectroscopy Standards]] and support the signal quality required by [[ECoG Signal Preprocessing Pipeline]].

### Connector Interface

The array connects to the wireless transmitter via a 96-pin flex cable:

- Gold ball-bond wire connections to array bond pads
- Polyimide flex cable, 25 um trace width
- Hermetic feedthrough connector compatible with [[Wireless Transmitter Power Budget]] transmitter module

### Design Inputs

Key design inputs derive from:

- User needs documented in [[Design History File Organization]]
- Signal quality requirements from [[Decoder Calibration Protocol]]
- Biocompatibility requirements per ISO 10993 (see [[Biocompatibility Test Results Summary]])
- Sterilization compatibility (EtO, gamma, or e-beam)

#electrode-design #specifications #silicon
`,
	},
	{
		Title:   "Cleanroom Process Parameter Optimization",
		Project: 5,
		Tags:    []string{"cleanroom", "process", "manufacturing"},
		Body: `## Cleanroom Process Parameter Optimization

This document tracks the optimization of critical process parameters in the ISO Class 5 cleanroom used for electrode array fabrication.

### Deep Reactive Ion Etching (DRIE)

The Bosch process for silicon needle formation is the most critical step:

| Parameter | Baseline | Optimized | Impact |
|-----------|----------|-----------|--------|
| SF6 flow (etch) | 130 sccm | 145 sccm | +12% etch rate |
| C4F8 flow (passivation) | 85 sccm | 90 sccm | Smoother sidewalls |
| Etch cycle time | 7.0 s | 6.5 s | Reduced scalloping |
| Passivation cycle time | 4.0 s | 4.5 s | Better profile control |
| Platen power | 12 W | 15 W | Improved anisotropy |
| Pressure | 35 mTorr | 30 mTorr | Sharper tips |

### Parylene-C Deposition

Conformal insulation coating parameters:

    Vaporizer temperature: 175 +/- 2 C
    Pyrolysis temperature: 690 +/- 5 C
    Chamber pressure: 22 +/- 2 mTorr
    Deposition rate: 0.5 um/hr (measured by QCM)
    Target thickness: 5.0 um

### SIROF Sputtering

Tip metallization for charge injection:

- Target: Iridium (99.99% purity)
- Argon flow: 20 sccm
- Oxygen flow: 5 sccm (reactive sputtering)
- RF power: 100 W
- Substrate temperature: ambient (no heating)

### Process Control Charts

All critical parameters are tracked via statistical process control (SPC). Out-of-spec conditions trigger a hold per the CAPA process referenced in [[ISO 13485 QMS Gap Analysis]]. Process changes require formal change control per [[Regulatory Submission Document Control]].

Yield improvements from these optimizations are tracked in [[Manufacturing Yield Analysis and Improvement]]. The resulting electrode properties are validated against [[Electrode Array Design Specifications]].

#drie #parylene #process-optimization
`,
	},
	{
		Title:   "Electrode Impedance Spectroscopy Standards",
		Project: 5,
		Tags:    []string{"impedance", "testing", "quality-control"},
		Body: `## Electrode Impedance Spectroscopy Standards

Impedance spectroscopy is the primary quality control method for verifying electrode array electrical performance before packaging.

### Test Equipment

- Potentiostat/Galvanostat: Gamry Reference 600+
- 3-electrode cell: Ag/AgCl reference, Pt counter, array working
- Electrolyte: Phosphate-buffered saline (PBS), pH 7.4, 37 C
- Faraday cage: Required for all measurements

### Test Protocol

1. Soak array in PBS at 37 C for minimum 30 minutes (hydration)
2. Open circuit potential (OCP) stabilization: wait until dV/dt < 1 mV/min
3. Electrochemical impedance spectroscopy (EIS):
    - Frequency range: 1 Hz to 100 kHz
    - AC amplitude: 10 mV RMS
    - Points per decade: 10
    - DC bias: 0 V vs OCP
4. Record impedance magnitude and phase at 1 kHz for pass/fail determination

### Acceptance Criteria

| Parameter | Specification | Action if Fail |
|-----------|--------------|----------------|
| Z at 1 kHz | 100 - 800 kOhm | Reject channel |
| Phase at 1 kHz | -60 to -80 degrees | Investigate coating |
| Channel yield | >= 90/96 channels pass | Accept array |
| Channel yield | < 90/96 channels pass | Reject array |
| Inter-channel CV | < 30% | Investigate process |

### Data Management

All impedance data is stored in the manufacturing database with traceability to:

- Array serial number per [[Electrode Array Lot Tracking and Traceability]]
- Cleanroom batch record per [[Cleanroom Process Parameter Optimization]]
- Operator ID and calibration certificate for test equipment

Results are compared against the design specifications in [[Electrode Array Design Specifications]] and the signal quality requirements of [[ECoG Signal Preprocessing Pipeline]].

#impedance #eis #quality-control #testing
`,
	},
	{
		Title:   "Biocompatibility Test Results Summary",
		Project: 5,
		Tags:    []string{"biocompatibility", "iso-10993", "testing"},
		Body: `## Biocompatibility Test Results Summary

This document summarizes biocompatibility testing results for the implantable electrode array per ISO 10993 series requirements for a permanently implanted device contacting the CNS.

### Required Tests (ISO 10993-1 Risk Assessment)

| Test | Standard | Lab | Status | Result |
|------|----------|-----|--------|--------|
| Cytotoxicity | ISO 10993-5 | Nelson Labs | Complete | Pass (Grade 0) |
| Sensitization | ISO 10993-10 | Nelson Labs | Complete | Pass (no reaction) |
| Irritation | ISO 10993-23 | Nelson Labs | Complete | Pass (score 0.0) |
| Systemic toxicity | ISO 10993-11 | NAMSA | Complete | Pass (no effects) |
| Genotoxicity (Ames) | ISO 10993-3 | NAMSA | Complete | Pass (non-mutagenic) |
| Genotoxicity (MN) | ISO 10993-3 | NAMSA | Complete | Pass (non-clastogenic) |
| Implantation (12-week) | ISO 10993-6 | In-house | Complete | Pass (minimal FBR) |
| Chronic toxicity | ISO 10993-11 | NAMSA | In progress | Pending (week 26) |
| Carcinogenicity | ISO 10993-3 | N/A | Justified waiver | Literature-based |

### Material Characterization

Per ISO 10993-18, chemical characterization was performed on:

- Silicon substrate: ICP-MS for trace metals, all below threshold of toxicological concern
- Parylene-C coating: GC-MS extractables study in PBS at 37 C (72-hour extraction)
- SIROF coating: XPS surface analysis confirming composition and oxidation state
- Polyimide flex cable: USP Class VI testing passed

### Foreign Body Response (12-Week Implant Study)

Six arrays implanted in rat motor cortex for 12 weeks. Histological analysis:

- Glial scar thickness: 25 +/- 8 um (acceptable, < 50 um threshold)
- Neuronal density at 100 um: 78 +/- 12% of contralateral control
- No evidence of chronic inflammation beyond expected FBR

These results support the risk assessment in [[Risk Management File Structure]] and are referenced in [[Clinical Evidence Strategy]] for regulatory submissions. Array lot traceability maintained per [[Electrode Array Lot Tracking and Traceability]].

#biocompatibility #iso-10993 #implant-safety
`,
	},
	{
		Title:   "Electrode Array Lot Tracking and Traceability",
		Project: 5,
		Tags:    []string{"traceability", "lot-tracking", "manufacturing"},
		Body: `## Electrode Array Lot Tracking and Traceability

Complete traceability from raw materials to implanted device is required for regulatory compliance and field safety corrective actions.

### Lot Numbering Convention

    EA-{Year}{Month}-{BatchSequence}-{ArraySequence}
    Example: EA-202603-B02-A015

    EA = Electrode Array
    202603 = March 2026
    B02 = Second batch of the month
    A015 = 15th array in the batch

### Traceability Chain

    Raw silicon wafer (supplier lot) ->
    DRIE processing (batch record) ->
    Parylene coating (batch record) ->
    SIROF deposition (batch record) ->
    Wire bonding (operator + equipment ID) ->
    Impedance testing per [[Electrode Impedance Spectroscopy Standards]] ->
    Visual inspection (pass/fail + images) ->
    Packaging and sterilization (sterilization lot) ->
    Device labeling per [[Device Labeling and UDI Requirements]] ->
    Shipment to clinical site ->
    Implantation record per [[Implant Surgery Coordination Checklist]]

### Database Schema

    CREATE TABLE electrode_lots (
        lot_id TEXT PRIMARY KEY,
        wafer_lot TEXT NOT NULL,
        drie_batch TEXT NOT NULL,
        parylene_batch TEXT NOT NULL,
        sirof_batch TEXT NOT NULL,
        bond_operator TEXT NOT NULL,
        impedance_report TEXT NOT NULL,
        visual_pass BOOLEAN NOT NULL,
        sterilization_lot TEXT,
        ship_date TEXT,
        destination_site TEXT,
        participant_id TEXT,
        created_at TEXT DEFAULT CURRENT_TIMESTAMP
    );

### Recall Capability

In the event of a field safety corrective action:

1. Query by any traceability parameter (e.g., wafer lot, DRIE batch)
2. Identify all affected arrays within 4 hours
3. Determine implant status (in stock, shipped, implanted)
4. Notify affected sites per [[Adverse Event Classification Framework]] escalation procedures

This traceability system satisfies requirements in [[ISO 13485 QMS Gap Analysis]] clause 7.5.3 and supports [[Post-Market Surveillance Plan]] field actions.

#traceability #lot-control #recall-readiness
`,
	},
	{
		Title:   "Manufacturing Yield Analysis and Improvement",
		Project: 5,
		Tags:    []string{"yield", "manufacturing", "quality"},
		Body: `## Manufacturing Yield Analysis and Improvement

This document tracks electrode array manufacturing yield metrics and ongoing improvement initiatives.

### Current Yield Summary (Q1 2026)

| Process Step | Input | Output | Yield | Cumulative |
|-------------|-------|--------|-------|------------|
| DRIE etching | 48 arrays/wafer | 44 | 91.7% | 91.7% |
| Parylene coating | 44 | 42 | 95.5% | 87.5% |
| SIROF deposition | 42 | 40 | 95.2% | 83.3% |
| Wire bonding | 40 | 36 | 90.0% | 75.0% |
| Impedance test | 36 | 32 | 88.9% | 66.7% |
| Visual inspection | 32 | 30 | 93.8% | 62.5% |
| **Overall** | **48** | **30** | -- | **62.5%** |

### Target: 80% cumulative yield by Q3 2026

### Root Cause Analysis: Top Yield Losses

**1. Wire Bonding (10% loss)**

- Primary failure: Ball bond lift-off at array bond pad
- Root cause: Pad metallization adhesion to Parylene sublayer
- Corrective action: Add Ti adhesion layer (10 nm) under Au bond pad
- Status: Process change validated, implementing in B04 batch

**2. Impedance Test (11.1% loss)**

- Primary failure: High impedance channels (> 800 kOhm)
- Root cause: Incomplete Parylene removal at electrode tips during O2 plasma etch-back
- Corrective action: Increase O2 plasma time from 90s to 120s per [[Cleanroom Process Parameter Optimization]]
- Status: DOE in progress

**3. DRIE Etching (8.3% loss)**

- Primary failure: Broken shanks during handling
- Root cause: Operator technique during wafer dicing
- Corrective action: Implement automated dicing saw with reduced feed rate

### Process Capability

Impedance at 1 kHz (all channels, last 10 batches):

- Mean: 380 kOhm
- Std dev: 145 kOhm
- Cpk: 0.96 (target > 1.33)

Process is not yet capable. Improvements per [[Electrode Impedance Spectroscopy Standards]] acceptance criteria are in progress.

#yield #improvement #process-capability
`,
	},
	{
		Title:   "Sterilization Validation Protocol",
		Project: 5,
		Tags:    []string{"sterilization", "validation", "manufacturing"},
		Body: `## Sterilization Validation Protocol

This document defines the sterilization validation strategy for the implantable electrode array, following ISO 11135 (EtO) and AAMI TIR12 guidelines.

### Selected Method: Ethylene Oxide (EtO)

EtO was selected based on material compatibility assessment:

| Material | EtO | Gamma | E-beam | Autoclave |
|----------|-----|-------|--------|-----------|
| Silicon | OK | OK | OK | OK |
| Parylene-C | OK | Degrades | Degrades | Melts |
| SIROF | OK | OK | OK | OK |
| Polyimide | OK | Yellows | Yellows | OK |
| Silicone (housing) | OK | OK | OK | OK |

Parylene-C degradation under ionizing radiation eliminates gamma and e-beam. The Parylene coating is essential per [[Electrode Array Design Specifications]].

### EtO Cycle Parameters

    Preconditioning: 24 hrs at 50 +/- 5 C, 60 +/- 10% RH
    Gas exposure: 600 +/- 30 mg/L EtO, 54 +/- 3 C, 2 hours
    Aeration: 48 hrs at 50 +/- 5 C (EtO residual dissipation)

### Validation Stages

1. **Installation Qualification (IQ)**: Verify sterilizer operates per manufacturer specifications
2. **Operational Qualification (OQ)**: Temperature and humidity mapping with empty and loaded chambers
3. **Performance Qualification (PQ)**: Three consecutive half-cycles with biological indicators

### Biological Indicator Challenge

- Organism: Bacillus atrophaeus (ATCC 9372)
- Population: >= 1 x 10^6 CFU per BI
- Placement: 12 positions per load, including worst-case locations
- Acceptance: Complete kill of all BIs at half-cycle exposure

### EtO Residual Testing

Per ISO 10993-7:

- Ethylene oxide: < 4 mg per device (24-hour extraction)
- Ethylene chlorohydrin: < 9 mg per device
- Testing performed on 3 devices per validation lot

### Post-Validation Monitoring

Routine sterility testing per ISO 11737-2 on every 10th production lot. Results linked to lot records per [[Electrode Array Lot Tracking and Traceability]]. Sterilization records are included in the [[Device Labeling and UDI Requirements]] package.

#sterilization #eto #validation
`,
	},
	{
		Title:   "Incoming Material Inspection Procedures",
		Project: 5,
		Tags:    []string{"incoming-inspection", "materials", "quality"},
		Body: `## Incoming Material Inspection Procedures

All raw materials and components used in electrode array manufacturing undergo incoming quality inspection before release to the cleanroom.

### Critical Materials

| Material | Supplier | Spec | Inspection Level |
|----------|----------|------|-----------------|
| Silicon wafers (100) | SVM (Slovak) | 525 +/- 25 um, p-type, 1-10 Ohm-cm | Full |
| Parylene-C dimer | SCS | Di-chloro-di-para-xylylene, 99.5% purity | Full |
| Iridium target | Kurt Lesker | 99.99% purity, 3" diameter | Certificate review |
| Au bond wire | Heraeus | 25 um diameter, 99.99% purity | Certificate review |
| Polyimide film | DuPont | Kapton HN, 25 um | Certificate review |
| Ti adhesion wire | Kurt Lesker | 99.995% purity | Certificate review |

### Full Inspection Protocol

For silicon wafers and Parylene-C dimer (critical materials):

1. **Visual inspection**: Check for chips, cracks, contamination
2. **Dimensional verification**: Wafer thickness by micrometer (5 points per wafer)
3. **Resistivity measurement**: 4-point probe (5 points per wafer)
4. **Certificate of Analysis (CoA) review**: Verify all parameters within specification
5. **Lot sampling**: AQL 1.0, General Inspection Level II per ANSI/ASQ Z1.4

### Acceptance and Rejection Criteria

    IF all parameters within spec AND CoA matches:
        -> Accept lot, update inventory, link to supplier lot number
    IF minor discrepancy (CoA data within spec but incomplete):
        -> Hold, request updated CoA from supplier
    IF any parameter out of spec:
        -> Reject lot, initiate supplier NCR (non-conformance report)

### Supplier Qualification

Suppliers are qualified per the process defined in [[ISO 13485 QMS Gap Analysis]] clause 7.4. Annual supplier audits are conducted for critical material suppliers. Performance metrics (delivery, quality, responsiveness) are tracked quarterly.

Material lot numbers link to the full traceability chain in [[Electrode Array Lot Tracking and Traceability]]. Any material substitution requires a formal change control review with assessment against [[Biocompatibility Test Results Summary]] and [[Risk Management File Structure]].

#incoming-inspection #materials #supplier-quality
`,
	},
	{
		Title:   "Wire Bonding Process Qualification",
		Project: 5,
		Tags:    []string{"wire-bonding", "process", "qualification"},
		Body: `## Wire Bonding Process Qualification

The wire bonding step connects the electrode array bond pads to the polyimide flex cable using thermosonic gold ball bonding. This is the highest yield-loss step and requires rigorous process qualification.

### Equipment

- Bonder: Kulicke & Soffa IConn ProCU PLUS
- Wire: 25 um Au (99.99%), Heraeus H11
- Capillary: SBNT-28ZA-AZM-1/16-XL

### Process Parameters

| Parameter | Nominal | Range | Impact |
|-----------|---------|-------|--------|
| Ultrasonic power | 85 mW | 75-95 mW | Bond shear strength |
| Bond force | 25 gf | 20-30 gf | Pad deformation |
| Bond time | 15 ms | 10-20 ms | Intermetallic formation |
| Stage temperature | 150 C | 140-160 C | Wire deformability |
| Loop height | 200 um | 150-250 um | Strain relief |
| Tail length | 50 um | 40-60 um | Ball formation |

### Qualification Protocol

1. **DOE (Design of Experiments)**: 2^3 factorial design on power, force, and time
2. **Response variables**: Ball shear strength (target > 8 gf), wire pull strength (target > 3 gf)
3. **Sample size**: 30 bonds per condition, 8 conditions = 240 bonds total
4. **Acceptance**: Cpk > 1.33 for both shear and pull at selected parameters

### Bond Integrity Testing

- **Ball shear test**: Dage 4000Plus, 100 um shear height, 50 um/s speed
- **Wire pull test**: Dage 4000Plus, mid-span pull, 200 um/s speed
- **Visual inspection**: 100% under 200x microscope (ball symmetry, tail shape, loop profile)

### Failure Mode Analysis

Common failure modes tracked against [[Manufacturing Yield Analysis and Improvement]]:

- Ball lift (adhesion failure at pad): Root cause typically pad contamination or insufficient US power
- Neck break (wire fracture at ball neck): Root cause typically excessive loop height or bond force
- Non-stick on pad (NSOP): Root cause typically oxide on pad surface

Process changes are controlled per [[Regulatory Submission Document Control]] and must be re-qualified if any critical parameter shifts outside the validated range. The Ti adhesion layer improvement referenced in yield analysis has been incorporated into [[Electrode Array Design Specifications]] rev C.

#wire-bonding #qualification #gold-ball
`,
	},
	{
		Title:   "Environmental Monitoring Program",
		Project: 5,
		Tags:    []string{"cleanroom", "environmental", "monitoring"},
		Body: `## Environmental Monitoring Program

This document defines the environmental monitoring program for the ISO Class 5 cleanroom where electrode arrays are fabricated.

### Particle Monitoring

| Classification | Max Particles >= 0.5 um/m3 | Max Particles >= 5.0 um/m3 |
|---------------|---------------------------|---------------------------|
| ISO Class 5 | 3,520 | 29 |
| Our target | < 2,500 | < 20 |

### Monitoring Schedule

| Parameter | Method | Frequency | Locations |
|-----------|--------|-----------|-----------|
| Airborne particles | Laser particle counter | Continuous (6 points) | DRIE, sputter, bond stations |
| Temperature | RTD sensors | Continuous | 4 corners + center |
| Humidity | Capacitive sensors | Continuous | 4 corners + center |
| Pressure differential | Manometer | Continuous | Room vs. corridor |
| Surface particles | Witness wafers | Weekly | All process stations |
| Viable organisms | Settle plates | Weekly | 6 locations |
| HEPA filter integrity | DOP/PAO test | Semi-annual | All filters |

### Environmental Specifications

    Temperature: 21 +/- 1 C
    Relative humidity: 45 +/- 5%
    Pressure differential: +12.5 Pa (min) vs. adjacent area
    Air changes: >= 300/hour (unidirectional flow)
    Lighting: >= 500 lux at work surface

### Excursion Response

If any parameter exceeds specification:

1. **Alert limit** (warning): Log event, increase monitoring frequency
2. **Action limit** (specification boundary): Stop production, investigate root cause
3. **Out-of-spec**: Quarantine all in-process material, initiate CAPA per [[ISO 13485 QMS Gap Analysis]]

### Gowning Protocol

All personnel entering the cleanroom must follow gowning SOP:

- Full bunny suit, hood, mask, goggles, double gloves, shoe covers
- Gowning room air shower (30 seconds minimum)
- Particle count verification on gown before entry (< 100 particles/ft2)

Environmental data is stored alongside batch records for traceability per [[Electrode Array Lot Tracking and Traceability]]. Cleanroom qualification reports are included in the [[Design History File Organization]] as part of manufacturing process validation.

#cleanroom #environmental #iso-class-5
`,
	},
	{
		Title:   "Accelerated Aging and Shelf Life Testing",
		Project: 5,
		Tags:    []string{"aging", "shelf-life", "reliability"},
		Body: `## Accelerated Aging and Shelf Life Testing

This protocol establishes the accelerated aging study design for determining electrode array shelf life and long-term implant reliability.

### Study Design

Per ASTM F1980 (Standard Guide for Accelerated Aging of Sterile Barrier Systems):

- **Accelerated aging temperature**: 55 C
- **Ambient reference temperature**: 22 C
- **Q10 factor**: 2.0 (conservative)
- **Aging factor**: Q10^((T_accelerated - T_ambient)/10) = 2^((55-22)/10) = 9.85

### Shelf Life Testing

| Target Shelf Life | Accelerated Duration | Sample Size |
|-------------------|---------------------|-------------|
| 1 year | 37 days | n=10 |
| 2 years | 74 days | n=10 |
| 3 years (target) | 111 days | n=10 |

### Test Endpoints at Each Timepoint

1. **Sterile barrier integrity**: ASTM F2095 bubble leak test, ASTM F1929 dye penetration
2. **Electrode impedance**: Full EIS per [[Electrode Impedance Spectroscopy Standards]]
3. **Mechanical integrity**: 10x insertion into agarose brain phantom
4. **Visual inspection**: SEM of electrode tips at 1000x magnification
5. **Parylene integrity**: Dielectric breakdown voltage testing

### Implant Longevity (Soak Test)

Separate from shelf life, accelerated soak testing simulates the implant environment:

    Medium: PBS at 87 C (acceleration factor ~32x at Q10=2)
    Duration: 90 days (equivalent to ~8 years at 37 C)
    Measurements: Weekly impedance, monthly SEM

### Acceptance Criteria

- Impedance change < 50% from baseline through target shelf life
- No sterile barrier failures at any timepoint
- No visible Parylene delamination or cracking
- Mechanical insertion force within specification

Results feed into the reliability section of [[Risk Management File Structure]] and support the shelf life claim on device labeling per [[Device Labeling and UDI Requirements]]. Long-term implant data is compared with clinical data from [[Long-Term Follow-Up Protocol]].

#aging #shelf-life #reliability #astm-f1980
`,
	},
	{
		Title:   "Electrode Tip Geometry Characterization",
		Project: 5,
		Tags:    []string{"metrology", "characterization", "quality"},
		Body: `## Electrode Tip Geometry Characterization

Electrode tip geometry directly impacts tissue penetration mechanics, signal recording quality, and chronic tissue response. This document defines the characterization methods and acceptance criteria.

### Critical Geometry Parameters

| Parameter | Target | Method | Frequency |
|-----------|--------|--------|-----------|
| Tip radius | < 5 um | SEM | 100% (sample per array) |
| Shaft taper angle | 3-5 degrees | SEM cross-section | Per batch |
| Electrode length | 1500 +/- 50 um | Optical profilometer | 100% |
| Surface roughness (Ra) | < 0.5 um (shaft) | AFM | Per batch |
| SIROF coverage | >= 95% of tip area | EDS mapping | Per batch |

### SEM Inspection Protocol

1. Mount array on 45-degree SEM stub with conductive carbon tape
2. Sputter coat with 5 nm Au/Pd (for non-conductive surfaces)
3. Image at 500x (full array overview), 2000x (individual electrodes), 10000x (tip detail)
4. Measure tip radius using tangent circle fitting algorithm
5. Document any defects: broken tips, bent shafts, coating anomalies

### Tip Sharpness and Insertion Mechanics

Sharper tips reduce insertion force and tissue dimpling:

    Insertion force model: F = k * r^2 * sigma_tissue
    where:
        k = geometric factor (~0.5 for conical tips)
        r = tip radius
        sigma_tissue = cortical tissue yield stress (~10 kPa)

    Target insertion force per electrode: < 1 mN
    Measured (r=3um): 0.45 mN (within specification)

### Correlation with Signal Quality

We track the relationship between tip geometry and in-vivo signal quality:

- Arrays with mean tip radius < 3 um show 15% higher SNR at 30 days post-implant
- This data feeds into [[Decoder Calibration Protocol]] expectations for new participants
- Tip geometry predicts initial impedance per [[Electrode Impedance Spectroscopy Standards]]

### Process Feedback Loop

SEM characterization results drive DRIE parameter adjustments in [[Cleanroom Process Parameter Optimization]]. Tip radius trending is reported in [[Manufacturing Yield Analysis and Improvement]] as a leading indicator of process drift.

#metrology #sem #tip-geometry #quality
`,
	},
	{
		Title:   "Packaging Design and Validation",
		Project: 5,
		Tags:    []string{"packaging", "validation", "sterile-barrier"},
		Body: `## Packaging Design and Validation

The electrode array packaging system must maintain sterility, protect against mechanical damage during shipping, and enable aseptic presentation in the operating room.

### Packaging Configuration

**Primary package (sterile barrier)**:
- Tyvek lid (1073B, 75 gsm) heat-sealed to PETG tray
- Tray cavity custom-molded to hold array in suspension
- Desiccant sachet (2g molecular sieve) included

**Secondary package**:
- Corrugated chipboard box with foam insert
- Array tray secured in cutout to prevent movement
- IFU, CoC, and UDI label per [[Device Labeling and UDI Requirements]]

**Shipping container**:
- Insulated carton with gel packs (2-25 C range)
- Temperature indicator (irreversible)
- Shock indicator (25G threshold)

### Seal Validation (ASTM F2029)

Heat seal parameters optimized via DOE:

| Parameter | Low | Center | High | Optimum |
|-----------|-----|--------|------|---------|
| Temperature (C) | 135 | 150 | 165 | 155 |
| Dwell time (s) | 1.0 | 1.5 | 2.0 | 1.5 |
| Pressure (psi) | 30 | 40 | 50 | 40 |

### Package Integrity Testing

| Test | Standard | Sample Size | Acceptance |
|------|----------|-------------|------------|
| Seal strength (peel) | ASTM F88 | n=15 per lot | 1.5-3.5 N/15mm |
| Bubble leak | ASTM F2095 | n=5 per lot | No bubbles at 55 kPa |
| Dye penetration | ASTM F1929 | n=5 per lot | No dye intrusion |
| Distribution simulation | ASTM D4169 | n=3 per design | No damage, seal intact |

### Worst-Case Challenges

Distribution simulation includes:

- Drop testing: 6 faces, 3 edges, 3 corners from 76 cm
- Vibration: Random vibration profile simulating 1000-mile truck transport
- Compression: 24-hour static load at 2x stack height

All testing coordinated with [[Accelerated Aging and Shelf Life Testing]] to validate shelf life claims. Packaging specifications are version-controlled per [[Regulatory Submission Document Control]] and traceability maintained through [[Electrode Array Lot Tracking and Traceability]].

#packaging #sterile-barrier #distribution
`,
	},
	{
		Title:   "Transformer Architecture for Neural Decoding",
		Project: 6,
		Tags:    []string{"deep-learning", "transformer", "architecture"},
		Body: `## Transformer Architecture for Neural Decoding

We are adapting the standard transformer architecture for continuous neural signal decoding. Unlike NLP tasks, our input sequences are multi-channel time series from Utah arrays (96 channels, 30kHz sampling).

### Key Modifications

- **Positional encoding**: Replaced sinusoidal encoding with learnable temporal embeddings that capture the 1-2ms binning structure of spike counts.
- **Channel attention**: Added a cross-channel attention layer before the standard self-attention block. This lets the model learn spatial relationships between electrodes without explicit wiring knowledge.
- **Causal masking**: For real-time decoding we enforce strict causal masking so the model never attends to future time bins.

### Input Representation

Each time bin is represented as a vector of dimension 96 (one per channel). We project this to d_model=256 via a linear layer. Context window is 500ms (25 bins at 50Hz after binning).

    class NeuralTransformer(nn.Module):
        def __init__(self, n_channels=96, d_model=256, n_heads=8, n_layers=4):
            super().__init__()
            self.input_proj = nn.Linear(n_channels, d_model)
            self.pos_embed = nn.Embedding(50, d_model)
            encoder_layer = nn.TransformerEncoderLayer(d_model, n_heads)
            self.encoder = nn.TransformerEncoder(encoder_layer, n_layers)
            self.decoder_head = nn.Linear(d_model, 7)  # 7-DOF output

### Performance

Early benchmarks show 15% improvement in R-squared over our previous Kalman filter baseline on the same offline dataset. Latency is under 8ms on A100 GPU. See [[Spike Sorting Pipeline v2]] for the upstream preprocessing and [[ECoG Signal Preprocessing Pipeline]] for the signal conditioning steps we adapted from the Cortical Decoder team.

Next steps involve integrating with the [[Training Pipeline Infrastructure]] and running participant-specific fine-tuning. #deep-learning #architecture
`,
	},
	{
		Title:   "Spike Sorting Pipeline v2",
		Project: 6,
		Tags:    []string{"spike-sorting", "preprocessing", "signal-processing"},
		Body: `## Spike Sorting Pipeline v2

Major overhaul of our spike sorting infrastructure to support real-time operation and integration with the [[Transformer Architecture for Neural Decoding]] model.

### Pipeline Stages

1. **Bandpass filtering**: 300Hz - 6kHz 4th-order Butterworth, applied per channel.
2. **Threshold detection**: Adaptive threshold at -4.5 sigma of the noise floor, recalculated every 30 seconds.
3. **Feature extraction**: First 3 principal components of each detected waveform (48 samples at 30kHz = 1.6ms window).
4. **Clustering**: Online Bayesian clustering using a Dirichlet process mixture model. Replaces the old k-means approach that required manual cluster count selection.
5. **Unit tracking**: Template matching across sessions to maintain consistent unit identities.

### Throughput Requirements

| Stage | Latency (per bin) | Throughput |
|-------|-------------------|------------|
| Filter | 0.2ms | 96 ch real-time |
| Detection | 0.1ms | 96 ch real-time |
| PCA | 0.5ms | ~200 spikes/bin |
| Clustering | 1.2ms | ~200 spikes/bin |

Total pipeline latency is under 2ms, well within our 20ms bin size for downstream decoding.

### Integration Notes

The sorted spike trains feed directly into the [[Neural Feature Extraction Methods]] module, which computes firing rates, ISI distributions, and population vectors. We also export raw threshold crossings to [[Electrode Impedance QC Protocol]] for hardware health monitoring.

Drift correction remains a challenge -- see [[Transfer Learning Across Participants]] for approaches to handle representational shift within sessions. #spike-sorting #signal-processing
`,
	},
	{
		Title:   "Training Pipeline Infrastructure",
		Project: 6,
		Tags:    []string{"infrastructure", "mlops", "training"},
		Body: `## Training Pipeline Infrastructure

Our ML training pipeline is built on PyTorch Lightning with custom data loaders for neural recording sessions. This document covers the end-to-end workflow from raw data to deployable model checkpoint.

### Data Flow

1. Raw recordings pulled from the [[BCI Data Warehouse Architecture]] via the data access API.
2. Preprocessing applies the [[Spike Sorting Pipeline v2]] offline (for training, we use the full non-causal version).
3. Feature matrices are cached as HDF5 files on local NVMe storage.
4. Training runs are orchestrated via Weights & Biases for experiment tracking.

### Hardware Setup

- 4x NVIDIA A100 (80GB) in a single node
- DDP training with gradient accumulation for large batch sizes
- Mixed precision (bf16) throughout

### Configuration

    training:
      batch_size: 256
      learning_rate: 3e-4
      weight_decay: 0.01
      warmup_steps: 1000
      max_epochs: 200
      early_stopping_patience: 15
      scheduler: cosine_with_restarts

### Experiment Tracking

Every run logs to W&B with the following metadata: participant ID (anonymized per [[Neural Data De-identification Protocol]]), session date hash, array location, task type, and model hyperparameters.

### Model Registry

Trained checkpoints are versioned and stored in the model registry. The [[Real-Time Inference Engine Design]] pulls the latest approved checkpoint for deployment. We follow the validation protocol in [[Offline Validation Dataset Curation]] before promoting any model to production.

Cross-team note: the [[REST API Specification]] from NeuroLink SDK will expose model metadata for external integrators. #mlops #training #infrastructure
`,
	},
	{
		Title:   "Transfer Learning Across Participants",
		Project: 6,
		Tags:    []string{"transfer-learning", "generalization", "neural-decoding"},
		Body: `## Transfer Learning Across Participants

One of our core research questions: can we pre-train a foundation model on pooled neural data and fine-tune it for new participants with minimal calibration?

### Motivation

Current decoders require 10-15 minutes of calibration data per session. If we can reduce this to under 2 minutes via transfer learning, it dramatically improves the user experience and clinical viability. The [[Decoder Calibration Protocol]] from Cortical Decoder documents the current calibration burden.

### Approach: Neural Foundation Model

We train a large [[Transformer Architecture for Neural Decoding]] on data pooled from all available participants (currently P01-P12). Key challenges:

- **Electrode alignment**: Each participant has a different array placement. We use a learned spatial normalization layer that maps arbitrary electrode geometries to a canonical 10x10 grid.
- **Neural variability**: Tuning curves differ across individuals. The first 2 transformer layers are shared (capturing general temporal dynamics), while later layers are participant-specific adapters.
- **Task alignment**: We standardize the output space across center-out reaching, grasp tasks, and cursor control.

### Results So Far

| Calibration Time | Baseline R2 | Transfer R2 |
|-----------------|-------------|-------------|
| 10 min | 0.72 | 0.78 |
| 5 min | 0.58 | 0.74 |
| 2 min | 0.31 | 0.67 |
| 30 sec | 0.12 | 0.51 |

The 2-minute transfer model outperforms the 10-minute baseline -- a strong result. See [[Participant P07 Session Notes]] for the detailed session where we first validated this.

Cross-referencing with [[Locomotion Decoder Design]] from Spinal Cord Interface, which has similar calibration challenges. #transfer-learning #generalization
`,
	},
	{
		Title:   "Neural Feature Extraction Methods",
		Project: 6,
		Tags:    []string{"features", "signal-processing", "neural-decoding"},
		Body: `## Neural Feature Extraction Methods

This note documents the feature representations we compute from sorted spike trains and LFP signals for downstream decoding models.

### Spike-Based Features

- **Firing rates**: Spike counts per 20ms bin, optionally smoothed with a 50ms Gaussian kernel. This is the primary input to the [[Transformer Architecture for Neural Decoding]].
- **ISI distributions**: Inter-spike interval histograms (log-spaced bins from 1ms to 500ms). Useful for detecting bursting vs tonic firing patterns.
- **Population vectors**: Cosine-tuned directional vectors computed from firing rates during the center-out task. Used as a baseline comparison.
- **Pair-wise correlations**: Noise correlations between simultaneously recorded units, computed in sliding 500ms windows.

### LFP-Based Features

- **Band power**: Spectral power in theta (4-8Hz), alpha (8-13Hz), beta (13-30Hz), low gamma (30-70Hz), and high gamma (70-200Hz) bands.
- **Phase-amplitude coupling**: Modulation index between theta phase and high-gamma amplitude. Strong PAC is associated with movement planning.

### Feature Selection

We use mutual information analysis to rank features by their predictive value for each DOF. Typically the top 30-40 units carry 80% of the decodable information. The [[Spike Sorting Pipeline v2]] output quality directly impacts feature reliability.

### Storage Format

Features are stored as structured arrays in HDF5:

    /session_001/
        firing_rates:  (T, N_units) float32
        lfp_power:     (T, N_channels, N_bands) float32
        timestamps:    (T,) float64

These feed into the [[Training Pipeline Infrastructure]] for model development and into the [[BCI Data Warehouse Architecture]] for long-term archival. #features #signal-processing
`,
	},
	{
		Title:   "Real-Time Inference Engine Design",
		Project: 6,
		Tags:    []string{"inference", "real-time", "deployment"},
		Body: `## Real-Time Inference Engine Design

The inference engine runs trained decoder models in real-time during BCI sessions. Strict latency requirements: end-to-end from neural data arrival to decoded output must be under 30ms.

### Architecture

The engine runs as a standalone C++/Python hybrid process:

- **C++ core**: Receives neural data over shared memory from the acquisition system. Performs the [[Spike Sorting Pipeline v2]] in real-time. Manages the inference thread pool.
- **Python model host**: Loads PyTorch models exported via TorchScript. The [[Training Pipeline Infrastructure]] produces these checkpoints.
- **Output interface**: Decoded kinematics are published via ZeroMQ to downstream consumers (e.g., the [[Closed-Loop Grasp Controller]] in Sensory Feedback Loop).

### Latency Budget

| Component | Budget | Measured |
|-----------|--------|----------|
| Data acquisition | 5ms | 3.2ms |
| Spike sorting | 2ms | 1.8ms |
| Feature extraction | 1ms | 0.7ms |
| Model inference | 8ms | 6.1ms |
| Output publish | 1ms | 0.4ms |
| **Total** | **17ms** | **12.2ms** |

### Fault Tolerance

- If inference exceeds 25ms, we fall back to a lightweight Kalman filter that runs in under 1ms.
- Model hot-swapping: new checkpoints can be loaded without stopping the session.
- All predictions are logged to the [[Session Telemetry and Monitoring]] system for post-hoc analysis.

### GPU Requirements

We target the NVIDIA Jetson Orin for bedside deployment (low power, adequate throughput). Cloud-based inference via the [[BCI Cloud Platform Compute Architecture]] is available for research sessions where latency tolerance is higher. #inference #real-time
`,
	},
	{
		Title:   "Offline Validation Dataset Curation",
		Project: 6,
		Tags:    []string{"validation", "datasets", "benchmarking"},
		Body: `## Offline Validation Dataset Curation

Maintaining a rigorous offline validation dataset is critical for comparing model architectures and detecting regressions. This note describes our curation process and dataset composition.

### Dataset Requirements

- Minimum 50 hours of recording across at least 8 participants
- Balanced representation of task types: center-out reaching (40%), grasp (30%), cursor control (20%), free movement (10%)
- Include sessions with known decoder degradation (electrode drift, noise events) to test robustness
- All data must pass the [[Neural Data De-identification Protocol]] before inclusion

### Current Composition

| Participant | Hours | Tasks | Array Type |
|------------|-------|-------|------------|
| P01 | 8.2 | reach, grasp | Utah 96ch |
| P03 | 6.7 | reach, cursor | Utah 96ch |
| P05 | 7.1 | reach, grasp, cursor | Utah 96ch |
| P07 | 9.3 | all | Utah 128ch |
| P08 | 5.4 | reach, grasp | Neuropixels |
| P09 | 4.8 | cursor, free | Utah 96ch |
| P10 | 6.2 | reach, grasp | Utah 96ch |
| P11 | 3.9 | grasp, cursor | Utah 128ch |

Total: 51.6 hours across 8 participants.

### Versioning

Datasets are versioned using DVC (Data Version Control) with storage in the [[BCI Data Warehouse Architecture]]. Each version is immutable once published. Current version: v2.3.

### Benchmark Protocol

All models are evaluated using 5-fold cross-validation within participants, plus leave-one-participant-out for [[Transfer Learning Across Participants]] evaluation. Metrics: R-squared, correlation coefficient, and bit rate. Results feed into the model comparison dashboard described in [[Analytics Dashboard Design]]. #validation #benchmarking
`,
	},
	{
		Title:   "Attention Mechanism Analysis for Neural Signals",
		Project: 6,
		Tags:    []string{"interpretability", "attention", "analysis"},
		Body: `## Attention Mechanism Analysis for Neural Signals

Understanding what our transformer models attend to is crucial for both scientific insight and clinical trust. This note documents our interpretability analysis of attention patterns in the [[Transformer Architecture for Neural Decoding]].

### Methods

We extract attention weights from all 8 heads across 4 layers during offline decoding of reach tasks. Analysis focuses on:

1. **Temporal attention profiles**: Which past time bins does the model attend to when predicting current kinematics?
2. **Channel attention patterns**: Which electrode channels receive the highest attention weights?
3. **Head specialization**: Do different heads learn distinct roles?

### Key Findings

**Temporal patterns**: The model primarily attends to time bins 40-120ms in the past, consistent with the known motor cortex lead time. Heads in layer 1 show broad temporal attention, while deeper layers focus on a narrow 60-80ms window.

**Channel attention**: Attention weights correlate strongly (r=0.82) with the directional tuning depth of each unit as measured by the [[Neural Feature Extraction Methods]] pipeline. Noisy or untuned channels receive near-zero attention -- the model effectively learns to ignore bad electrodes without explicit quality flags.

**Head specialization**: We observe clear functional specialization:
- Heads 1-2: Velocity encoding (attend to recent bins, velocity-tuned channels)
- Heads 3-4: Position encoding (broader temporal window)
- Heads 5-6: Cross-channel coordination patterns
- Heads 7-8: Appear to track state transitions (rest to move onset)

### Clinical Implications

This analysis supports using attention maps as a diagnostic tool. If attention patterns shift unexpectedly, it may indicate electrode degradation before it shows up in impedance measurements from [[Electrode Impedance QC Protocol]]. We are building a monitoring dashboard for this -- see [[Session Telemetry and Monitoring]]. #interpretability #attention
`,
	},
	{
		Title:   "Data Augmentation Strategies for Neural Recordings",
		Project: 6,
		Tags:    []string{"augmentation", "training", "data"},
		Body: `## Data Augmentation Strategies for Neural Recordings

Limited training data is a persistent bottleneck in BCI model development. Each participant provides at most a few hours of labeled data. We have developed several augmentation strategies specific to neural signals.

### Techniques

#### 1. Channel Dropout
Randomly zero out 5-15% of channels during training. This improves robustness to electrode failures, which are common in chronic implants. Analogous to the dropout regularization concept but applied at the input level.

#### 2. Temporal Jitter
Shift spike times by +/- 1 sample (33 microseconds at 30kHz). This is below the temporal resolution that matters for decoding and adds useful noise to prevent overfitting to exact spike timing.

#### 3. Firing Rate Scaling
Multiply firing rates by a random factor drawn from N(1.0, 0.15). Simulates gain changes that occur naturally across sessions, supporting the [[Transfer Learning Across Participants]] work.

#### 4. Synthetic Mixtures
Blend features from two different sessions of the same participant with random interpolation weights. Requires aligned task structures from the [[Decoder Calibration Protocol]].

#### 5. Neural Style Transfer
Use a CycleGAN to translate neural activity patterns between participants while preserving kinematics labels. Experimental -- results are mixed.

### Ablation Study

| Augmentation | R2 Improvement | p-value |
|-------------|----------------|---------|
| None (baseline) | -- | -- |
| Channel dropout | +0.04 | 0.003 |
| Temporal jitter | +0.01 | 0.21 |
| Rate scaling | +0.03 | 0.008 |
| Synthetic mix | +0.05 | 0.001 |
| All combined | +0.09 | <0.001 |

All combined augmentation yields a 12% relative improvement in R-squared. These augmented datasets are managed through the [[Training Pipeline Infrastructure]] and validated against the [[Offline Validation Dataset Curation]] benchmark. #augmentation #training
`,
	},
	{
		Title:   "Neural Data De-identification Protocol",
		Project: 6,
		Tags:    []string{"privacy", "de-identification", "compliance"},
		Body: `## Neural Data De-identification Protocol

All neural data used for model training must be de-identified per HIPAA Safe Harbor guidelines. This protocol documents our process, which is coordinated with the [[Encryption Standards for Neural Data]] and [[HIPAA Compliance Framework for Neural Data]] teams.

### De-identification Steps

1. **Participant ID replacement**: Replace real participant IDs with sequential codes (P01, P02, ...). Mapping table stored in a separate, access-controlled system.
2. **Date shifting**: All timestamps are shifted by a random offset (per participant) of 30-365 days. Relative timing within sessions is preserved.
3. **Demographic scrubbing**: Remove age, sex, diagnosis details, and implant date from metadata. Retain only: array type, electrode count, cortical region.
4. **Session note redaction**: Free-text clinical notes are excluded entirely. Only structured task labels and performance metrics are retained.
5. **Neural signature analysis**: We run a re-identification risk assessment to ensure that neural firing patterns alone cannot identify a participant. Current k-anonymity: k >= 5 across our cohort.

### Implementation

    def deidentify_session(session, shift_days, new_id):
        session.participant_id = new_id
        session.timestamps += timedelta(days=shift_days)
        session.metadata = filter_safe_harbor(session.metadata)
        session.clinical_notes = None
        return session

### Data Flow

Raw identified data lives only on the secure clinical server (never in the training pipeline). The de-identification script runs as a pre-processing step before data enters the [[Training Pipeline Infrastructure]] or the [[BCI Data Warehouse Architecture]].

All de-identified datasets must be approved by the data governance board before use. See [[IRB Protocol Amendments]] for the latest approved data sharing agreement and [[Data Anonymization Techniques for Neural Recordings]] for the underlying algorithmic approaches. #privacy #de-identification
`,
	},
	{
		Title:   "Model Compression for Edge Deployment",
		Project: 6,
		Tags:    []string{"compression", "edge", "optimization"},
		Body: `## Model Compression for Edge Deployment

Deploying decoder models on bedside hardware (NVIDIA Jetson Orin or similar) requires significant model compression. Our full [[Transformer Architecture for Neural Decoding]] has 12M parameters -- too large for real-time inference within power constraints.

### Compression Techniques

#### Knowledge Distillation
Train a smaller student model (1.2M params) to match the outputs of the full teacher model. The student uses 2 transformer layers instead of 4 and d_model=128 instead of 256. Distillation loss is a weighted combination of task loss and KL divergence on logits.

#### Quantization
Post-training quantization from FP32 to INT8 using TensorRT. Results:

| Model | Size | Latency (Orin) | R2 |
|-------|------|-----------------|-----|
| Full FP32 | 48MB | 14ms | 0.78 |
| Full INT8 | 12MB | 4.2ms | 0.77 |
| Distilled FP32 | 5MB | 3.8ms | 0.74 |
| Distilled INT8 | 1.3MB | 1.1ms | 0.73 |

#### Structured Pruning
Remove entire attention heads that contribute least to decoding accuracy. The [[Attention Mechanism Analysis for Neural Signals]] work identifies which heads are expendable. Pruning heads 7-8 (state transition tracking) has minimal impact on steady-state decoding.

### Deployment Integration

Compressed models are exported via the [[Real-Time Inference Engine Design]] pipeline. The [[Implant Firmware OTA Update Protocol]] from the Telemetry team handles model updates on implanted systems, though current implants run simpler algorithms on-chip.

Power consumption of the distilled INT8 model on Orin: 3.2W average during active decoding, well within the bedside unit's thermal budget. #compression #edge #optimization
`,
	},
	{
		Title:   "Continual Learning for Decoder Adaptation",
		Project: 6,
		Tags:    []string{"continual-learning", "adaptation", "online-learning"},
		Body: `## Continual Learning for Decoder Adaptation

Neural signals drift over time due to electrode micro-motion, glial encapsulation, and neural plasticity. Static decoders degrade significantly over weeks to months. This note describes our continual learning approach to maintain decoder performance without explicit recalibration.

### Problem Statement

Day-to-day variability in neural recordings means a model trained on Day 1 may lose 20-30% accuracy by Day 30. The traditional approach is periodic recalibration (see [[Decoder Calibration Protocol]]), but this is burdensome for participants.

### Our Approach: Retrospective Replay with Elastic Weight Consolidation

1. **Unsupervised alignment**: Use the decoded output to infer intended movements (assuming the user achieves their goal most of the time). This provides pseudo-labels without explicit calibration.
2. **Nightly updates**: After each session, run a short fine-tuning pass on the day's data using the pseudo-labels.
3. **EWC regularization**: Elastic Weight Consolidation prevents catastrophic forgetting of earlier participant-specific tuning from the [[Transfer Learning Across Participants]] initialization.
4. **Replay buffer**: Maintain a buffer of 1000 representative samples from past sessions. Replay 20% of each training batch from this buffer.

### Monitoring

We track decoder stability metrics through the [[Session Telemetry and Monitoring]] system:

- Daily R-squared on standardized task blocks
- Weight drift magnitude (L2 distance from initial checkpoint)
- Replay buffer diversity score

### Results

Over a 90-day evaluation with participant P07 (see [[Participant P07 Session Notes]]):

- Without adaptation: R2 dropped from 0.78 to 0.49
- With nightly EWC updates: R2 maintained at 0.71-0.78

This dramatically extends the useful life of the decoder between clinical visits. The approach integrates with the [[Motor Recovery Progress Tracking]] system in Neurorehab to distinguish genuine motor recovery from signal drift. #continual-learning #adaptation
`,
	},
	{
		Title:   "Multi-Task Decoder Architecture",
		Project: 6,
		Tags:    []string{"multi-task", "architecture", "decoding"},
		Body: `## Multi-Task Decoder Architecture

Rather than training separate decoders for reaching, grasping, speech, and cursor control, we are developing a unified multi-task architecture that shares a common neural representation backbone.

### Architecture Design

    Input (96ch spike rates) --> Shared Encoder (4 transformer layers)
        |
        +--> Reach Head (2 layers --> 3D velocity)
        +--> Grasp Head (2 layers --> 5 finger forces)
        +--> Cursor Head (1 layer --> 2D velocity)
        +--> Speech Head (3 layers --> phoneme logits)

The shared encoder learns a task-agnostic neural representation. Task-specific heads are lightweight adapters. Task selection is determined by a context signal (explicit mode switch by the user or automatic via a [[Low-Latency Intent Classification]] module from BCI Gaming).

### Benefits

- **Data efficiency**: The shared encoder sees data from all tasks, enabling better feature learning with limited per-task data.
- **Transfer**: Adding a new task requires only training a new head (~100K params) while keeping the shared encoder frozen.
- **Consistency**: A single model checkpoint to manage in the [[Training Pipeline Infrastructure]].

### Training Strategy

We use a round-robin multi-task training schedule:
1. Sample a task uniformly at random
2. Sample a batch from that task's dataset
3. Forward through shared encoder + task-specific head
4. Backpropagate task loss through both head and encoder

Gradient conflict between tasks is managed via PCGrad (projecting conflicting gradients). This is important because reach and grasp tasks can have opposing gradient directions in the shared layers.

### Integration

The multi-task model feeds into the [[Real-Time Inference Engine Design]] with dynamic head selection. The [[Phoneme Decoder Architecture]] from Speech Prosthesis informed our speech head design. Future work will integrate the [[Locomotion Decoder Design]] from Spinal Cord Interface as an additional head. #multi-task #architecture
`,
	},
	{
		Title:   "Hyperparameter Optimization Framework",
		Project: 6,
		Tags:    []string{"hyperparameters", "optimization", "mlops"},
		Body: `## Hyperparameter Optimization Framework

Systematic hyperparameter search is essential given the sensitivity of neural decoders to configuration choices. This note documents our Optuna-based optimization framework integrated with the [[Training Pipeline Infrastructure]].

### Search Space

| Parameter | Range | Scale |
|-----------|-------|-------|
| learning_rate | 1e-5 to 1e-2 | log |
| d_model | {128, 256, 512} | categorical |
| n_heads | {4, 8, 16} | categorical |
| n_layers | 2 to 8 | int |
| dropout | 0.0 to 0.4 | uniform |
| weight_decay | 1e-4 to 0.1 | log |
| batch_size | {64, 128, 256, 512} | categorical |
| context_window | 10 to 50 bins | int |

### Optimization Strategy

We use Tree-structured Parzen Estimator (TPE) with the following setup:

- **Objective**: Maximize mean R-squared on the [[Offline Validation Dataset Curation]] holdout set (leave-one-participant-out)
- **Trials**: 200 trials with median pruning (stop unpromising runs early)
- **Parallelism**: 4 concurrent trials across our GPU cluster
- **Budget**: Each trial trains for max 50 epochs (~2 hours)

### Best Configuration Found

    best_params:
      learning_rate: 2.7e-4
      d_model: 256
      n_heads: 8
      n_layers: 4
      dropout: 0.15
      weight_decay: 0.012
      batch_size: 256
      context_window: 25

This configuration is used as the default for the [[Transformer Architecture for Neural Decoding]] and [[Multi-Task Decoder Architecture]].

### Per-Participant Fine-Tuning

After the global HPO, we run a smaller 30-trial search for participant-specific parameters (primarily learning rate and dropout). This is part of the [[Transfer Learning Across Participants]] adaptation pipeline. Results are tracked in the [[Analytics Dashboard Design]] dashboards. #hyperparameters #optimization
`,
	},
	{
		Title:   "BCI Cloud Platform Compute Architecture",
		Project: 7,
		Tags:    []string{"cloud", "architecture", "compute"},
		Body: `## BCI Cloud Platform Compute Architecture

The BCI Cloud Platform provides shared compute infrastructure for all research groups in the lab. This document describes the overall architecture and how different workloads are orchestrated.

### Infrastructure Overview

We run on AWS with the following core services:

- **EKS cluster**: Kubernetes for containerized workloads (API servers, data processors, dashboards)
- **GPU nodes**: p4d.24xlarge instances for model training, auto-scaling 0-8 nodes based on queue depth
- **Storage**: S3 for raw data archival, EFS for shared model checkpoints
- **Database**: RDS PostgreSQL for metadata, Amazon Timestream for time-series telemetry

### Workload Categories

1. **Real-time telemetry ingestion**: Streaming data from active BCI sessions via WebSocket. Latency-critical. See [[Session Telemetry and Monitoring]].
2. **Batch training**: GPU-intensive model training jobs from [[Training Pipeline Infrastructure]] in Neural Signal AI. Scheduled during off-peak hours.
3. **Analytics**: Dashboard queries and report generation. See [[Analytics Dashboard Design]].
4. **Data processing**: ETL pipelines for the [[BCI Data Warehouse Architecture]].

### Network Architecture

Each clinical site connects via AWS Direct Connect (1Gbps dedicated link) or VPN fallback. Data in transit is encrypted using the protocols defined in [[Encryption Standards for Neural Data]]. The [[RF Link Budget Analysis]] from Implant Telemetry constrains the upstream bandwidth from implant to local acquisition system.

### Cost Management

Monthly spend target: $45K. GPU training is the dominant cost (60%). We use Spot instances for training with checkpointing every 10 minutes to handle interruptions. Reserved instances for always-on services. #cloud #architecture
`,
	},
	{
		Title:   "Session Telemetry and Monitoring",
		Project: 7,
		Tags:    []string{"telemetry", "monitoring", "real-time"},
		Body: `## Session Telemetry and Monitoring

Real-time monitoring of BCI sessions is critical for participant safety and research data quality. This system captures, transmits, and visualizes telemetry from active sessions across all clinical sites.

### Data Streams

Each active session generates the following telemetry:

- **Neural signal quality**: Per-channel SNR, noise floor, spike rates (updated every 1s)
- **Decoder performance**: Real-time R-squared, decoded velocity traces, error metrics
- **System health**: CPU/GPU utilization, memory, network latency, buffer occupancy
- **Participant state**: Task performance metrics, fatigue indicators, session duration

### Architecture

    Clinical Site --> WebSocket --> Ingestion Service --> Timestream
                                        |
                                  Redis (live state)
                                        |
                                  Dashboard (Grafana)

The ingestion service runs as a Kubernetes deployment on the [[BCI Cloud Platform Compute Architecture]] with horizontal auto-scaling based on active session count.

### Alert Rules

| Condition | Severity | Action |
|-----------|----------|--------|
| Channel SNR < 3dB on > 20% channels | Warning | Notify researcher |
| Decoder R2 < 0.3 for > 60s | Critical | Suggest recalibration |
| Network latency > 500ms | Warning | Switch to local recording |
| System memory > 90% | Critical | Auto-restart non-essential services |

### Integration Points

- The [[Real-Time Inference Engine Design]] publishes decoder metrics to this system
- The [[Wireless Transmitter Power Budget]] from Cortical Decoder affects available telemetry bandwidth
- Session recordings are archived to the [[BCI Data Warehouse Architecture]] after session end
- The [[Remote Session Control Interface]] allows researchers to act on alerts #telemetry #monitoring
`,
	},
	{
		Title:   "BCI Data Warehouse Architecture",
		Project: 7,
		Tags:    []string{"data-warehouse", "storage", "architecture"},
		Body: `## BCI Data Warehouse Architecture

The data warehouse is the central repository for all BCI research data across the lab. It stores raw recordings, processed features, model outputs, and clinical metadata.

### Schema Design

We use a star schema optimized for analytical queries:

**Fact tables**:
- session_recordings: Raw and processed neural data references (S3 paths)
- decoder_metrics: Per-bin decoder performance metrics
- clinical_events: Timestamped clinical observations

**Dimension tables**:
- participants: De-identified participant info (per [[Neural Data De-identification Protocol]])
- sessions: Session metadata (date, duration, task type, site)
- electrodes: Array configuration, channel mapping, impedance history
- models: Decoder model versions and hyperparameters

### Storage Tiers

| Tier | Storage | Retention | Data |
|------|---------|-----------|------|
| Hot | EFS + Timestream | 30 days | Active session data |
| Warm | S3 Standard | 1 year | Recent recordings, features |
| Cold | S3 Glacier | 7 years | Archived raw data (regulatory) |

Data lifecycle transitions are automated via S3 lifecycle policies. The 7-year cold retention meets FDA requirements per [[FDA 510k Submission Timeline]].

### Access Control

Role-based access with project-level granularity:
- Researchers can access only their project's data
- Cross-project access requires PI approval and is logged
- All queries are audited per [[Audit Logging and Access Controls]]

### Query Interface

Researchers query the warehouse via a SQL interface (Presto/Trino) or the Python SDK. The [[Analytics Dashboard Design]] connects via the same query engine. Data export requires compliance check against [[HIPAA Compliance Framework for Neural Data]]. #data-warehouse #storage
`,
	},
	{
		Title:   "Analytics Dashboard Design",
		Project: 7,
		Tags:    []string{"analytics", "dashboard", "visualization"},
		Body: `## Analytics Dashboard Design

The analytics dashboard provides researchers and PIs with visual insights into decoder performance, data quality, and research progress across all projects.

### Dashboard Modules

#### 1. Decoder Performance Tracker
- R-squared trends over time per participant
- Cross-participant comparison heatmaps
- Regression detection alerts (from [[Session Telemetry and Monitoring]])

#### 2. Data Quality Monitor
- Electrode impedance trends (data from [[Electrode Impedance QC Protocol]])
- Signal-to-noise ratio distributions
- Spike sorting quality metrics from [[Spike Sorting Pipeline v2]]

#### 3. Research Progress
- Training experiment tracker (W&B integration)
- Model version comparison on [[Offline Validation Dataset Curation]] benchmarks
- Publication and milestone timeline

#### 4. Clinical Operations
- Session scheduling and utilization rates
- Per-site activity summary (multi-site data from [[Multi-Site Data Synchronization Protocol]])
- Adverse event tracking (linked to [[Adverse Event Reporting SOP]])

### Technical Stack

- **Frontend**: Grafana with custom panels
- **Data source**: Presto queries against the [[BCI Data Warehouse Architecture]]
- **Real-time**: Timestream datasource for live session metrics
- **Auth**: SSO via lab LDAP, role-based dashboard access

### Key Metrics

    SELECT
      participant_id,
      session_date,
      AVG(r_squared) as mean_r2,
      COUNT(DISTINCT channel_id) as active_channels,
      SUM(spike_count) / session_duration as mean_firing_rate
    FROM decoder_metrics
    JOIN sessions USING (session_id)
    WHERE session_date >= CURRENT_DATE - INTERVAL '30' DAY
    GROUP BY participant_id, session_date
    ORDER BY session_date DESC

Dashboards are accessible to external collaborators via the read-only portal described in [[SDK Architecture Overview]] from NeuroLink SDK. #analytics #dashboard
`,
	},
	{
		Title:   "Multi-Site Data Synchronization Protocol",
		Project: 7,
		Tags:    []string{"multi-site", "synchronization", "data"},
		Body: `## Multi-Site Data Synchronization Protocol

With BCI research sessions running at 4 clinical sites, consistent data synchronization is essential. This protocol defines how data flows from clinical sites to the central [[BCI Data Warehouse Architecture]].

### Site Configuration

| Site | Location | Connection | Daily Volume |
|------|----------|------------|--------------|
| Site A | Main Campus | Direct Connect 1Gbps | 50-100 GB |
| Site B | Partner Hospital | VPN (500Mbps) | 30-60 GB |
| Site C | Rehab Center | VPN (200Mbps) | 20-40 GB |
| Site D | International | VPN (100Mbps) | 10-20 GB |

### Synchronization Flow

1. **Local buffer**: Each site runs a local data collector that buffers session data on encrypted local storage.
2. **Incremental sync**: After session completion, data is synced to S3 via AWS DataSync. Only changed blocks are transferred (deduplication at 4MB chunk level).
3. **Metadata registration**: Session metadata is registered in the warehouse via the [[Cloud Platform API Gateway Design]] REST endpoint.
4. **Validation**: Automated checks verify data completeness (all expected channels present, no gaps > 1s, checksum match).
5. **Notification**: Researchers are notified via the [[Analytics Dashboard Design]] when their data is available.

### Conflict Resolution

Rare but possible: two sites upload data for the same participant if they transfer between sites mid-study. Resolution:
- Each session has a globally unique ID (ULID)
- Duplicate detection based on session timestamp + participant + site
- Manual review required for true conflicts (flagged in [[Multi-Site Coordination Handbook]])

### Bandwidth Management

Site D has limited bandwidth. For this site, we prioritize sync of decoded features over raw neural data. Raw data is synced during overnight windows. This is aligned with the data tiering strategy in the warehouse. #multi-site #synchronization
`,
	},
	{
		Title:   "Cloud Platform API Gateway Design",
		Project: 7,
		Tags:    []string{"api", "gateway", "cloud"},
		Body: `## Cloud Platform API Gateway Design

The API gateway is the single entry point for all programmatic access to the BCI Cloud Platform. It handles authentication, rate limiting, request routing, and audit logging.

### Endpoint Structure

    /api/v1/
        /sessions              # Session management
        /sessions/{id}/data    # Neural data access
        /sessions/{id}/metrics # Decoder performance
        /models                # Model registry
        /models/{id}/deploy    # Model deployment
        /warehouse/query       # Ad-hoc data queries
        /telemetry/stream      # WebSocket for live telemetry

### Authentication

JWT-based authentication with refresh tokens. Integration with the lab's LDAP for user identity. Service-to-service communication uses mTLS. Token scopes:

- 'read:sessions' -- view session metadata and metrics
- 'write:sessions' -- create and modify sessions
- 'read:data' -- access raw neural data (requires [[HIPAA Compliance Framework for Neural Data]] training completion)
- 'admin:models' -- deploy and manage models
- 'admin:platform' -- platform administration

### Rate Limiting

| Scope | Limit | Window |
|-------|-------|--------|
| Standard user | 100 req/min | Per user |
| Data download | 10 req/min | Per user |
| WebSocket | 5 connections | Per user |
| Service account | 1000 req/min | Per service |

### Integration with NeuroLink SDK

The [[REST API Specification]] from NeuroLink SDK wraps these endpoints with a higher-level client library. The [[Beta Program Rollout Plan]] defines the timeline for external API access.

### Audit Logging

Every API call is logged to the [[Audit Logging and Access Controls]] system with: timestamp, user identity, endpoint, response code, data volume, and source IP. This is a regulatory requirement per [[ISO 13485 Quality Manual]]. #api #gateway
`,
	},
	{
		Title:   "Remote Session Control Interface",
		Project: 7,
		Tags:    []string{"remote-control", "session-management", "clinical"},
		Body: `## Remote Session Control Interface

Enables researchers and clinicians to monitor and control BCI sessions remotely. Critical for multi-site operations where the lead researcher may not be physically present at the clinical site.

### Capabilities

- **View**: Real-time neural signal visualization, decoder output traces, participant camera feed (encrypted)
- **Control**: Start/stop recording, trigger calibration routines, adjust decoder parameters (gain, smoothing)
- **Communicate**: Text and voice chat with on-site clinical staff
- **Annotate**: Mark events in the recording timeline (artifacts, behavioral observations)

### Security Model

Remote control is a sensitive capability. Security layers:

1. **Authentication**: Multi-factor authentication required (not just JWT)
2. **Authorization**: Only the session PI and designated co-investigators can access remote control
3. **Confirmation**: Destructive actions (stop session, modify decoder) require on-site staff confirmation via a separate channel
4. **Audit**: All remote actions are logged in [[Audit Logging and Access Controls]] with full context

### Technical Implementation

The interface is a React web application served by the [[BCI Cloud Platform Compute Architecture]]. Real-time data is streamed via WebSocket through the [[Cloud Platform API Gateway Design]].

Video feeds are encrypted end-to-end using the protocols in [[Encryption Standards for Neural Data]]. Frame rate is adaptive based on available bandwidth (1-15 fps).

### Clinical Protocol Integration

Remote sessions follow the same protocol as in-person sessions per the [[Phase I Trial Design]] requirements. Any decoder parameter changes made remotely are flagged for review in the [[Multi-Site Coordination Handbook]]. #remote-control #session-management
`,
	},
	{
		Title:   "Platform Disaster Recovery Plan",
		Project: 7,
		Tags:    []string{"disaster-recovery", "reliability", "operations"},
		Body: `## Platform Disaster Recovery Plan

Defines recovery procedures and targets for the BCI Cloud Platform. Active BCI sessions involve participant safety, so recovery time is critical.

### Recovery Objectives

| System | RTO | RPO | Priority |
|--------|-----|-----|----------|
| Live session telemetry | 5 min | 0 (no data loss) | P0 |
| API gateway | 15 min | 0 | P0 |
| Data warehouse | 4 hours | 1 hour | P1 |
| Analytics dashboards | 8 hours | 24 hours | P2 |
| Training pipeline | 24 hours | Last checkpoint | P3 |

### Backup Strategy

- **Database**: RDS automated backups every 6 hours, cross-region replication to us-west-2
- **S3 data**: Cross-region replication enabled for all buckets. Versioning ON.
- **Kubernetes**: Cluster configuration in Git (GitOps via ArgoCD). Can rebuild from scratch in 30 minutes.
- **Secrets**: Stored in AWS Secrets Manager with cross-region replication

### Failover Procedures

#### Active Session Failover
If the primary region becomes unavailable during an active session:
1. Local acquisition systems continue recording to local disk (always-on fallback)
2. Telemetry buffers locally for up to 4 hours
3. Once connectivity is restored, buffered data syncs per [[Multi-Site Data Synchronization Protocol]]

This is critical because participant safety cannot depend on cloud availability. The [[Real-Time Inference Engine Design]] runs locally for exactly this reason.

#### Data Warehouse Failover
The read replica in us-west-2 can be promoted to primary within 15 minutes. Query endpoints are updated via DNS failover.

### Testing

Disaster recovery drills are conducted quarterly. Results are documented and shared with the [[ISO 13485 Quality Manual]] audit team. Last drill: January 2026 -- full region failover completed in 22 minutes. #disaster-recovery #reliability
`,
	},
	{
		Title:   "Kubernetes Resource Management for BCI Workloads",
		Project: 7,
		Tags:    []string{"kubernetes", "resource-management", "infrastructure"},
		Body: `## Kubernetes Resource Management for BCI Workloads

Managing compute resources across diverse BCI workloads requires careful Kubernetes configuration. This note documents our resource policies and scheduling strategies on the [[BCI Cloud Platform Compute Architecture]].

### Namespace Organization

    bci-platform/
        telemetry/       # Real-time session monitoring
        training/        # GPU training jobs
        analytics/       # Dashboard and query services
        data-pipeline/   # ETL and sync services
        gateway/         # API gateway and auth

### Resource Quotas

| Namespace | CPU Limit | Memory Limit | GPU Limit |
|-----------|-----------|--------------|-----------|
| telemetry | 16 cores | 32 GB | 0 |
| training | 96 cores | 384 GB | 8 A100 |
| analytics | 8 cores | 16 GB | 0 |
| data-pipeline | 16 cores | 64 GB | 0 |
| gateway | 8 cores | 16 GB | 0 |

### Priority Classes

1. **session-critical** (priority 1000): Active session telemetry. Never preempted.
2. **training-standard** (priority 100): Scheduled training jobs. Can be preempted by session-critical.
3. **batch-low** (priority 10): Analytics refresh, data migration. Runs when resources available.

### GPU Scheduling

Training jobs from the [[Training Pipeline Infrastructure]] are submitted as Kubernetes Jobs with GPU resource requests. We use the NVIDIA GPU Operator for device plugin management.

    resources:
      requests:
        nvidia.com/gpu: 4
        memory: 80Gi
      limits:
        nvidia.com/gpu: 4
        memory: 96Gi

### Monitoring

Resource utilization is tracked in [[Analytics Dashboard Design]] with alerts for:
- GPU utilization below 50% for > 1 hour (wasteful scheduling)
- Memory pressure in telemetry namespace (risks data loss)
- Pod restart counts exceeding threshold

Node auto-scaling is configured with a 5-minute cooldown and a maximum of 12 nodes total across all pools. Cost tracking per namespace feeds into monthly budget reviews. #kubernetes #infrastructure
`,
	},
	{
		Title:   "Data Pipeline Orchestration",
		Project: 7,
		Tags:    []string{"data-pipeline", "orchestration", "etl"},
		Body: `## Data Pipeline Orchestration

The BCI Cloud Platform runs several data pipelines that transform raw session data into analysis-ready datasets. This note documents the orchestration layer.

### Pipeline Inventory

| Pipeline | Trigger | Duration | Frequency |
|----------|---------|----------|-----------|
| Session ingest | Session end | 5-30 min | Per session |
| Feature extraction | Session ingest complete | 10-60 min | Per session |
| FTS index update | Feature extraction complete | 2-5 min | Per session |
| Daily aggregation | Cron 02:00 UTC | 30-90 min | Daily |
| Model evaluation | New model checkpoint | 2-4 hours | On demand |
| Data quality audit | Cron 06:00 UTC | 15-30 min | Daily |

### Orchestration Engine

We use Apache Airflow running on the [[BCI Cloud Platform Compute Architecture]] Kubernetes cluster. DAGs are defined in Python and version-controlled in Git.

    session_ingest_dag:
        ingest_raw >> validate_data >> extract_features >>
        [update_fts_index, update_warehouse, notify_researcher]

### Session Ingest Pipeline (Detail)

1. **Ingest**: Pull raw recording from site storage (see [[Multi-Site Data Synchronization Protocol]])
2. **Validate**: Check data completeness, verify checksums, run format compliance
3. **De-identify**: Apply [[Neural Data De-identification Protocol]] if not already done
4. **Extract features**: Run [[Spike Sorting Pipeline v2]] and [[Neural Feature Extraction Methods]]
5. **Store**: Write processed data to [[BCI Data Warehouse Architecture]]
6. **Index**: Update search indices and metadata catalog
7. **Notify**: Push notification to researchers via dashboard and email

### Error Handling

Failed pipeline steps are retried 3 times with exponential backoff. After 3 failures, the pipeline is paused and an alert is sent. All pipeline runs are logged and visible in the [[Analytics Dashboard Design]] operations view. The [[Consumer EEG Signal Quality]] team from Non-Invasive EEG uses a simplified version of this pipeline for their headset data. #data-pipeline #orchestration
`,
	},
	{
		Title:   "Platform Capacity Planning",
		Project: 7,
		Tags:    []string{"capacity", "planning", "infrastructure"},
		Body: `## Platform Capacity Planning

Projecting infrastructure needs for the next 12 months based on research expansion plans and data growth trends.

### Current Utilization (as of Feb 2026)

| Resource | Capacity | Current Use | Utilization |
|----------|----------|-------------|-------------|
| GPU hours/month | 5,760 | 3,200 | 56% |
| Storage (S3) | Unlimited | 48 TB | N/A |
| EKS nodes (CPU) | 12 | 6 avg | 50% |
| Concurrent sessions | 20 | 8 peak | 40% |
| API requests/day | 500K | 180K | 36% |

### Growth Projections

**Q2 2026**: Two new clinical sites come online (Sites E and F). Expected impact:
- +40% session volume
- +60% data ingest bandwidth
- +30% storage growth rate

**Q3 2026**: Neural Signal AI team scaling up [[Transfer Learning Across Participants]] experiments:
- +100% GPU demand (need 8 A100s sustained instead of burst)
- Consider Reserved Instance commitment for cost savings

**Q4 2026**: External collaborator access via [[Beta Program Rollout Plan]] from NeuroLink SDK:
- +200% API traffic
- Need to add CDN for static assets and cached query results

### Recommended Actions

1. **Immediate**: Upgrade Site D VPN to 500Mbps (currently bottlenecked per [[Multi-Site Data Synchronization Protocol]])
2. **Q2**: Add 2 GPU nodes to EKS cluster, pre-purchase Reserved Instances
3. **Q3**: Implement query result caching layer in [[Cloud Platform API Gateway Design]]
4. **Q4**: Evaluate multi-region active-active deployment for international collaborators

### Budget Impact

Projected monthly spend increase: $45K (current) to $68K (Q4 2026). The GPU Reserved Instance commitment saves ~35% vs on-demand pricing. Storage costs grow linearly at ~$500/month per TB added. #capacity #planning
`,
	},
	{
		Title:   "HIPAA Compliance Framework for Neural Data",
		Project: 8,
		Tags:    []string{"hipaa", "compliance", "privacy"},
		Body: `## HIPAA Compliance Framework for Neural Data

Neural recordings constitute Protected Health Information (PHI) under HIPAA. This framework documents our compliance posture and ongoing requirements.

### Why Neural Data is PHI

Neural recordings are inherently identifiable:
- Electrode placement is participant-specific (from surgical planning)
- Neural firing patterns may be unique identifiers (analogous to fingerprints)
- Associated with diagnosis and treatment information

Therefore, ALL neural data in our systems must be treated as PHI unless explicitly de-identified per the [[Neural Data De-identification Protocol]].

### HIPAA Requirements Mapping

| HIPAA Rule | Our Implementation |
|------------|-------------------|
| Privacy Rule (use/disclosure) | Role-based access in [[Audit Logging and Access Controls]] |
| Security Rule (safeguards) | [[Encryption Standards for Neural Data]], network segmentation |
| Breach Notification | Automated detection + 72hr notification SOP |
| Minimum Necessary | Query-level access controls in [[BCI Data Warehouse Architecture]] |
| BAA Requirements | All cloud vendors (AWS, Ollama hosting) under BAA |

### Technical Safeguards

- **Access control**: MFA for all systems handling PHI. See [[Cloud Platform API Gateway Design]] for API-level controls.
- **Encryption**: AES-256 at rest, TLS 1.3 in transit. Details in [[Encryption Standards for Neural Data]].
- **Audit logs**: All PHI access logged and retained for 6 years. See [[Audit Logging and Access Controls]].
- **Data segmentation**: Participant data isolated at the storage level. Cross-participant queries require explicit authorization.

### Training Requirements

All personnel accessing neural data must complete HIPAA training annually. Completion is tracked and is a prerequisite for receiving API credentials (enforced in the [[Cloud Platform API Gateway Design]] provisioning workflow).

See [[IRB Protocol Amendments]] for the intersection of HIPAA compliance with our IRB-approved protocols. #hipaa #compliance
`,
	},
	{
		Title:   "Encryption Standards for Neural Data",
		Project: 8,
		Tags:    []string{"encryption", "security", "standards"},
		Body: `## Encryption Standards for Neural Data

This document defines the encryption standards applied to all neural data across the lab's systems. These standards satisfy both [[HIPAA Compliance Framework for Neural Data]] and [[ISO 13485 Quality Manual]] requirements.

### Data at Rest

| System | Algorithm | Key Length | Key Management |
|--------|-----------|------------|----------------|
| S3 buckets | AES-256-GCM | 256-bit | AWS KMS (CMK) |
| RDS databases | AES-256-CBC | 256-bit | AWS KMS (CMK) |
| Local workstations | LUKS2 (Linux) / FileVault (Mac) | 256-bit | User passphrase + recovery key |
| Backup media | AES-256-GCM | 256-bit | HSM-stored keys |

### Data in Transit

- **API traffic**: TLS 1.3 only. TLS 1.2 is disabled. Certificate pinning for mobile/embedded clients.
- **WebSocket telemetry**: WSS (WebSocket Secure) with the same TLS 1.3 configuration.
- **Site-to-cloud**: IPsec VPN (AES-256-GCM, SHA-384, DH Group 20) or AWS Direct Connect with MACsec.
- **Implant RF link**: AES-128-CCM (constrained by implant processor). See [[RF Link Budget Analysis]] for bandwidth implications of encryption overhead.

### Key Rotation

- AWS KMS CMKs: Automatic annual rotation
- TLS certificates: 90-day rotation via Let's Encrypt
- VPN keys: Rotated quarterly
- Implant keys: Rotated during firmware updates per [[Implant Firmware OTA Update Protocol]]

### Implementation Notes

For the [[BCI Cloud Platform Compute Architecture]], all inter-service communication within the Kubernetes cluster uses mTLS via Istio service mesh. This ensures encryption even for internal traffic.

Neural data exported for model training (via [[Training Pipeline Infrastructure]]) retains encryption until loaded into GPU memory. The decryption key is injected as a Kubernetes secret, never stored on disk.

### Compliance Verification

Quarterly encryption audits verify all standards are met. Results feed into [[Audit Logging and Access Controls]] and are available for [[FDA 510k Submission Timeline]] regulatory review. #encryption #security
`,
	},
	{
		Title:   "Data Anonymization Techniques for Neural Recordings",
		Project: 8,
		Tags:    []string{"anonymization", "privacy", "techniques"},
		Body: `## Data Anonymization Techniques for Neural Recordings

Building on the [[Neural Data De-identification Protocol]] from Neural Signal AI, this note explores advanced anonymization techniques specific to neural data.

### The Neural Fingerprinting Problem

Recent research has shown that neural activity patterns can serve as biometric identifiers with 95%+ accuracy across sessions. This means simple metadata scrubbing is insufficient -- the signal itself is identifying.

### Anonymization Techniques

#### 1. Differential Privacy for Spike Trains
Add calibrated Laplacian noise to binned spike counts. Privacy budget epsilon = 1.0 provides strong guarantees while preserving decoder-relevant features (tested on [[Offline Validation Dataset Curation]] benchmark):

    def dp_spike_counts(counts, epsilon=1.0):
        sensitivity = 1.0  # max change from one spike
        noise_scale = sensitivity / epsilon
        return counts + np.random.laplace(0, noise_scale, counts.shape)

R-squared impact: -0.03 (acceptable for most analyses).

#### 2. Channel Permutation
Randomly permute electrode channel assignments. Destroys spatial identity while preserving temporal dynamics. Must be applied consistently within a session.

#### 3. Temporal Warping
Apply random time-warping to neural sequences. Preserves rate information but disrupts fine temporal patterns that could be identifying.

#### 4. Feature-Space Anonymization
Instead of sharing raw signals, share only extracted features (firing rates, band power) that have been shown to be less identifying. The [[Neural Feature Extraction Methods]] pipeline produces these.

### Re-identification Risk Assessment

We evaluate anonymization effectiveness using a leave-one-session-out identification test:

| Technique | Identification Accuracy | R2 Impact |
|-----------|------------------------|-----------|
| None | 97% | 0 |
| DP (eps=1.0) | 34% | -0.03 |
| Channel permutation | 12% | -0.01 |
| Temporal warping | 28% | -0.05 |
| Combined | 8% | -0.07 |

Combined techniques reduce re-identification to near-chance levels. These methods feed into our [[Neurorights and Ethical Framework]] considerations. #anonymization #privacy
`,
	},
	{
		Title:   "Audit Logging and Access Controls",
		Project: 8,
		Tags:    []string{"audit", "access-control", "compliance"},
		Body: `## Audit Logging and Access Controls

Comprehensive audit logging is a cornerstone of our security and compliance posture. Every interaction with neural data is logged, retained, and auditable.

### What We Log

| Event Category | Examples | Retention |
|---------------|----------|-----------|
| Authentication | Login, logout, MFA challenge, failed attempts | 3 years |
| Data access | Query execution, file download, API calls | 6 years |
| Data modification | Upload, delete, de-identify, re-process | 6 years |
| Administrative | User provisioning, permission changes, key rotation | 6 years |
| System | Service start/stop, configuration changes, deployments | 1 year |

### Log Format

    {
      "timestamp": "2026-02-15T14:23:01Z",
      "event_type": "data_access",
      "user_id": "researcher_042",
      "resource": "session/01HPQR5T8K/neural_data",
      "action": "read",
      "data_volume_bytes": 52428800,
      "source_ip": "10.0.3.47",
      "result": "allowed",
      "policy_evaluated": "project_member_read"
    }

### Access Control Model

We use Attribute-Based Access Control (ABAC) with the following attributes:

- **Subject**: User role, project membership, HIPAA training status, MFA status
- **Resource**: Data classification level, project ownership, de-identification status
- **Action**: Read, write, delete, export, share
- **Context**: Source network, time of day, session state

Example policy: A researcher can read neural data only if they are a member of the owning project, have completed HIPAA training (per [[HIPAA Compliance Framework for Neural Data]]), have active MFA, and are on the lab network or VPN.

### Integration

Audit logs are shipped to a SIEM (Splunk) for anomaly detection. Quarterly access reviews are conducted per [[ISO 13485 Quality Manual]] requirements. The [[Cloud Platform API Gateway Design]] enforces access policies at the API layer. #audit #access-control
`,
	},
	{
		Title:   "Neurorights and Ethical Framework",
		Project: 8,
		Tags:    []string{"neurorights", "ethics", "policy"},
		Body: `## Neurorights and Ethical Framework

As BCI technology advances, we must proactively address the ethical implications of reading and potentially writing neural information. This framework establishes our lab's position on neurorights.

### Core Neurorights Principles

1. **Mental privacy**: Participants have the right to keep their neural data private. No decoded thought content should be stored or shared without explicit, informed consent.
2. **Cognitive liberty**: Participants retain full autonomy over their cognitive processes. BCI systems must never impose involuntary cognitive modifications.
3. **Mental integrity**: Protection against unauthorized manipulation of neural activity. Relevant for our [[Micro-stimulation Parameter Space]] work in Sensory Feedback Loop.
4. **Psychological continuity**: BCI interventions should not alter personal identity. Important consideration for the [[Adaptive Difficulty Algorithm]] in Neurorehab Therapy Suite.

### Implementation in Our Systems

- **Consent granularity**: Participants can consent to specific uses of their data (decoder training, research sharing, publication) independently. Consent records are stored in the [[Audit Logging and Access Controls]] system.
- **Data sovereignty**: Participants can request deletion of all their neural data at any time. This propagates through the [[BCI Data Warehouse Architecture]] and all derived datasets.
- **Decode boundaries**: Our decoders are trained only for the specific motor outputs consented to. We do not train or deploy models that decode emotional state, cognitive content, or other non-consented modalities.
- **Stimulation safeguards**: Closed-loop stimulation (per [[Closed-Loop Grasp Controller]]) has hard-wired safety limits that cannot be overridden by software.

### Regulatory Landscape

Several jurisdictions are developing neurorights legislation:
- Chile: Constitutional amendment (2021) -- right to mental integrity
- EU: Under consideration in AI Act amendments
- US: No federal legislation yet, but FDA guidance evolving (see [[FDA 510k Submission Timeline]])

Our framework exceeds current regulatory requirements. Reviewed annually by our ethics advisory board and the IRB (see [[IRB Protocol Amendments]]). #neurorights #ethics
`,
	},
	{
		Title:   "IRB Data Handling Requirements",
		Project: 8,
		Tags:    []string{"irb", "data-handling", "compliance"},
		Body: `## IRB Data Handling Requirements

Our Institutional Review Board (IRB) has specific requirements for how neural data is collected, stored, processed, and shared. This note consolidates all data handling requirements from our approved protocols.

### Approved Data Uses

Per our current IRB approval (Protocol #2024-BCI-037, amended per [[IRB Protocol Amendments]]):

1. **Primary analysis**: Decoder development and evaluation for the consented motor task
2. **Secondary analysis**: Cross-participant studies with de-identified data (per [[Neural Data De-identification Protocol]])
3. **Data sharing**: De-identified data may be shared with approved collaborators under a Data Use Agreement
4. **Publication**: Aggregate results and de-identified exemplar data may be published

### Prohibited Uses

- Training AI models for non-motor decoding (e.g., emotion, cognition) without separate consent
- Sharing identified data outside the research team
- Commercial use of participant data without separate commercial consent
- Linking neural data with external databases (social media, genetic, etc.)

### Data Retention Requirements

| Data Type | Minimum Retention | Maximum Retention | Storage |
|-----------|-------------------|-------------------|---------|
| Raw recordings | 7 years post-study | 10 years | Encrypted cold storage |
| De-identified datasets | 7 years post-study | Indefinite | [[BCI Data Warehouse Architecture]] |
| Consent documents | 7 years post-study | Indefinite | Secure document management |
| Audit logs | 6 years | 6 years | [[Audit Logging and Access Controls]] |

### Participant Withdrawal

If a participant withdraws:
1. Stop all data collection immediately
2. Retain existing data unless participant requests deletion (per consent terms)
3. Exclude from future analyses
4. Document withdrawal in the [[Adverse Event Reporting SOP]] if related to an adverse event

### Compliance Monitoring

Quarterly self-audits verify adherence. Annual IRB continuing review includes data handling audit results. All findings tracked in the [[HIPAA Compliance Framework for Neural Data]] compliance dashboard. #irb #data-handling
`,
	},
	{
		Title:   "Incident Response Plan for Data Breaches",
		Project: 8,
		Tags:    []string{"incident-response", "breach", "security"},
		Body: `## Incident Response Plan for Data Breaches

This plan defines procedures for detecting, containing, and recovering from data security incidents involving neural data.

### Incident Classification

| Level | Description | Example | Response Time |
|-------|-------------|---------|---------------|
| P0 - Critical | Active exfiltration of identified neural data | Compromised researcher credentials downloading PHI | 15 min |
| P1 - High | Unauthorized access to neural data systems | Unusual API access pattern detected | 1 hour |
| P2 - Medium | Policy violation without data exposure | Researcher accessed wrong project's data | 4 hours |
| P3 - Low | Potential vulnerability identified | Outdated TLS version on internal service | 24 hours |

### Detection Mechanisms

- **SIEM alerts**: Anomaly detection on access patterns from [[Audit Logging and Access Controls]]
- **DLP monitoring**: Data Loss Prevention on network egress points
- **User reports**: Researchers can report suspicious activity via secure channel
- **Automated scanning**: Daily vulnerability scans of all internet-facing services

### Response Procedure (P0/P1)

1. **Detect and triage** (0-15 min): Security team validates the alert, classifies severity
2. **Contain** (15-60 min): Revoke compromised credentials, isolate affected systems, preserve forensic evidence
3. **Assess** (1-4 hours): Determine scope of data exposure, identify affected participants
4. **Notify** (within 72 hours): Per HIPAA Breach Notification Rule:
   - Notify affected participants
   - Notify HHS if > 500 individuals affected
   - Notify IRB (per [[IRB Protocol Amendments]])
5. **Remediate** (1-7 days): Patch vulnerability, rotate credentials, update [[Encryption Standards for Neural Data]] if needed
6. **Post-incident review** (within 14 days): Root cause analysis, update controls

### Integration Points

- The [[Cloud Platform API Gateway Design]] implements the credential revocation endpoint
- The [[Platform Disaster Recovery Plan]] covers system restoration after containment
- Breach forensics may require analysis of the [[BCI Data Warehouse Architecture]] access logs
- The [[EU MDR Classification]] has separate breach notification requirements for European participants #incident-response #breach
`,
	},
	{
		Title:   "Network Security Architecture",
		Project: 8,
		Tags:    []string{"network-security", "architecture", "infrastructure"},
		Body: `## Network Security Architecture

Defense-in-depth network security design for the BCI research infrastructure, protecting neural data across clinical sites and cloud environments.

### Network Zones

    Zone 1 (Clinical): Implant <-> Acquisition System
        |  (air-gapped during session, sync post-session)
    Zone 2 (Site LAN): Acquisition <-> Local Processing
        |  (VPN / Direct Connect)
    Zone 3 (Cloud DMZ): API Gateway, Load Balancers
        |  (internal only)
    Zone 4 (Cloud Internal): Compute, Storage, Databases

### Zone Policies

| From -> To | Allowed | Protocol |
|------------|---------|----------|
| Zone 1 -> 2 | Data sync only | USB/Ethernet (encrypted) |
| Zone 2 -> 3 | API calls, data upload | HTTPS, WSS |
| Zone 3 -> 4 | Authenticated requests | mTLS |
| Zone 4 -> 4 | Service mesh | mTLS (Istio) |
| Any -> Zone 1 | BLOCKED | N/A |
| Internet -> Zone 3 | API only (authenticated) | HTTPS |

### Key Controls

- **Zone 1 isolation**: The clinical acquisition system is air-gapped during active sessions. No inbound connections. This protects against remote interference with the [[Real-Time Inference Engine Design]].
- **Micro-segmentation**: Within Zone 4, each project's data is in a separate VPC. Cross-project traffic requires explicit security group rules and is logged per [[Audit Logging and Access Controls]].
- **WAF**: AWS WAF in front of Zone 3 with rules for OWASP Top 10, rate limiting, and geo-blocking (only allow expected countries).
- **IDS/IPS**: Suricata running on VPN concentrators for site-to-cloud traffic inspection.

### Monitoring

Network flow logs are retained for 90 days and analyzed by the SIEM. Alerts for:
- Unexpected outbound connections from Zone 4
- Port scanning activity
- DNS exfiltration attempts
- Large data transfers outside business hours

All network architecture decisions align with [[HIPAA Compliance Framework for Neural Data]] and [[ISO 13485 Quality Manual]] requirements. The [[Wireless Transmitter Power Budget]] from Cortical Decoder defines the RF security requirements for Zone 1. #network-security #architecture
`,
	},
	{
		Title:   "Secure Data Sharing Protocol",
		Project: 8,
		Tags:    []string{"data-sharing", "security", "collaboration"},
		Body: `## Secure Data Sharing Protocol

Defines procedures for safely sharing neural data with external collaborators while maintaining privacy and regulatory compliance.

### Sharing Tiers

| Tier | Data Type | Approval Required | Agreement |
|------|-----------|-------------------|-----------|
| Tier 1 | Published results, aggregate statistics | None | N/A |
| Tier 2 | De-identified feature matrices | PI approval | Data Use Agreement |
| Tier 3 | De-identified raw recordings | PI + IRB approval | DUA + HIPAA BAA |
| Tier 4 | Identified data | PI + IRB + Participant consent | Full research collaboration agreement |

### Sharing Workflow

1. **Request**: External collaborator submits data request via the [[Cloud Platform API Gateway Design]] portal
2. **Classification**: Data steward classifies the request into a sharing tier
3. **Approval**: Route through appropriate approval chain
4. **Preparation**: Apply required de-identification per [[Data Anonymization Techniques for Neural Recordings]]
5. **Transfer**: Encrypted transfer via secure file sharing (not email, not USB)
6. **Tracking**: Log the share event in [[Audit Logging and Access Controls]], set data expiration date
7. **Follow-up**: Quarterly check that collaborator still needs the data, has maintained security

### Technical Controls

Data shared externally is:
- Encrypted with a recipient-specific key (GPG or age)
- Watermarked with an invisible identifier linking it to the specific share event
- Accompanied by a machine-readable data use agreement that specifies permitted uses
- Time-limited: access expires after the agreed period (default 1 year)

### Integration with NeuroLink SDK

The [[SDK Architecture Overview]] includes a data sharing module that automates Tier 1 and Tier 2 sharing. Tier 3 and 4 require manual workflow. The [[Beta Program Rollout Plan]] will be the first test of external data sharing at scale.

All sharing must comply with [[IRB Data Handling Requirements]] and the [[HIPAA Compliance Framework for Neural Data]]. Cross-border sharing has additional requirements per [[EU MDR Classification]]. #data-sharing #collaboration
`,
	},
	{
		Title:   "Vulnerability Management Program",
		Project: 8,
		Tags:    []string{"vulnerability", "security", "operations"},
		Body: `## Vulnerability Management Program

Systematic identification, assessment, and remediation of security vulnerabilities across all BCI research systems.

### Scope

All systems that process, store, or transmit neural data:
- Cloud infrastructure (AWS accounts, Kubernetes clusters)
- Clinical site workstations and acquisition systems
- Network equipment (VPN concentrators, firewalls)
- Software dependencies (application libraries, OS packages)
- Custom application code

### Scanning Schedule

| Scan Type | Frequency | Tool | Scope |
|-----------|-----------|------|-------|
| Infrastructure | Weekly | AWS Inspector | All EC2, EKS, RDS |
| Container images | On build | Trivy | All Docker images |
| Dependencies | Daily | Dependabot + Snyk | All repositories |
| Web applications | Monthly | OWASP ZAP | API gateway, dashboards |
| Penetration test | Annually | External firm | Full infrastructure |

### Severity and SLA

| Severity | CVSS Score | Remediation SLA | Examples |
|----------|------------|-----------------|---------|
| Critical | 9.0-10.0 | 24 hours | RCE in internet-facing service |
| High | 7.0-8.9 | 7 days | Auth bypass, SQL injection |
| Medium | 4.0-6.9 | 30 days | XSS, information disclosure |
| Low | 0.1-3.9 | 90 days | Minor misconfiguration |

### Patch Management

- **OS patches**: Applied within SLA via automated patching (AWS Systems Manager)
- **Application patches**: Tested in staging before production deployment
- **Kubernetes patches**: Rolling updates via ArgoCD, zero-downtime
- **Implant firmware**: Special process per [[Implant Firmware OTA Update Protocol]] -- cannot risk bricking implanted devices

### Reporting

Monthly vulnerability report is generated and reviewed by the security team. Findings feed into:
- [[Audit Logging and Access Controls]] for compliance tracking
- [[Incident Response Plan for Data Breaches]] if an active exploit is detected
- [[ISO 13485 Quality Manual]] for quality management system documentation

Exception requests for deferred patches require written justification and PI approval. #vulnerability #security
`,
	},
	{
		Title:   "Consent Management System Design",
		Project: 8,
		Tags:    []string{"consent", "privacy", "design"},
		Body: `## Consent Management System Design

Digital consent management for BCI research participants, supporting granular consent for different data uses and easy withdrawal.

### Consent Categories

Each participant can independently consent to or decline:

1. **Primary research**: Use of their data for the specific study they enrolled in
2. **Secondary research**: Use of de-identified data for other BCI studies within the lab
3. **External sharing**: Sharing de-identified data with approved external collaborators (per [[Secure Data Sharing Protocol]])
4. **Model training**: Use of data for training AI models (per [[Training Pipeline Infrastructure]])
5. **Publication**: Use of de-identified exemplar data in publications
6. **Long-term archival**: Retention of data beyond the study period for future research
7. **Commercial use**: Use of de-identified data for commercial product development

### System Architecture

    Participant Portal --> Consent Service --> Consent Database
                                |
                          Policy Engine --> Data Access Layer
                                |
                          [[Audit Logging and Access Controls]]

The policy engine evaluates consent status before any data access. If a participant has not consented to a specific use, the data is programmatically inaccessible for that purpose.

### Consent Lifecycle

- **Initial consent**: Obtained during enrollment, recorded digitally with timestamp and version
- **Modification**: Participants can modify consent at any time via the portal
- **Withdrawal**: Full withdrawal triggers the protocol in [[IRB Data Handling Requirements]]
- **Re-consent**: Required when study protocol changes (per [[IRB Protocol Amendments]])

### Technical Requirements

- Consent records are immutable (append-only log)
- Every consent change triggers an audit event
- Consent status is cached in the [[Cloud Platform API Gateway Design]] for low-latency enforcement
- Backup consent records are stored separately from neural data

### Integration with Neurorights

The consent system is designed to support the principles in our [[Neurorights and Ethical Framework]], particularly mental privacy and cognitive liberty. Participants can see exactly what their data has been used for via an activity log in the portal. #consent #privacy
`,
	},
	{
		Title:   "Regulatory Data Requirements Matrix",
		Project: 8,
		Tags:    []string{"regulatory", "compliance", "matrix"},
		Body: `## Regulatory Data Requirements Matrix

Consolidated view of data requirements across all regulatory frameworks applicable to our BCI research.

### Requirements Matrix

| Requirement | HIPAA | FDA | EU MDR | IRB | ISO 13485 |
|------------|-------|-----|--------|-----|-----------|
| Data encryption at rest | Required | Recommended | Required | Required | Required |
| Data encryption in transit | Required | Recommended | Required | Required | Required |
| Access audit logging | Required (6yr) | Required | Required | Required (6yr) | Required |
| Data retention minimum | 6 years | 7 years | 10 years | Per protocol | Per QMS |
| Breach notification | 72 hours | 72 hours | 72 hours | Immediate | Per procedure |
| De-identification | Safe Harbor / Expert | N/A | Pseudonymization | Per protocol | N/A |
| Risk assessment | Required | Required | Required | Required | Required |
| Training documentation | Required | Required | Required | Required | Required |

### How We Comply

- **Encryption**: [[Encryption Standards for Neural Data]] meets the strictest requirement (AES-256)
- **Audit logging**: [[Audit Logging and Access Controls]] retains logs for 6 years (satisfying all frameworks)
- **Data retention**: We default to 10 years (EU MDR maximum) -- see [[BCI Data Warehouse Architecture]] storage tiers
- **Breach notification**: [[Incident Response Plan for Data Breaches]] targets 72 hours for all frameworks
- **De-identification**: [[Data Anonymization Techniques for Neural Recordings]] goes beyond HIPAA Safe Harbor

### Regulatory Tracking

Each regulatory submission has specific data requirements:
- [[FDA 510k Submission Timeline]]: Clinical data package with full traceability
- [[EU MDR Classification]]: Technical documentation with clinical evaluation
- [[ISO 13485 Quality Manual]]: Quality records and design history file

### Gap Analysis

Current gaps (being addressed):

1. EU MDR requires a Data Protection Impact Assessment (DPIA) -- in progress, due Q2 2026
2. ISO 13485 requires formal document control for all data procedures -- migrating from wiki to controlled document system
3. FDA requires 21 CFR Part 11 compliance for electronic records -- implementing electronic signatures in the [[Consent Management System Design]]

Quarterly compliance reviews update this matrix. #regulatory #compliance
`,
	},
	{
		Title:   "Dry Electrode Material Selection",
		Project: 9,
		Tags:    []string{"hardware", "electrodes", "materials"},
		Body: `## Dry Electrode Material Selection

### Overview

Selecting the right electrode material is critical for achieving acceptable signal-to-noise ratio without conductive gel. We evaluated three candidate materials across key metrics relevant to consumer EEG.

### Candidate Materials

| Material | Contact Impedance (kOhm) | Comfort Rating | Durability (cycles) | Cost ($/unit) |
|----------|--------------------------|----------------|----------------------|---------------|
| Ag/AgCl coated pins | 15-40 | 3/5 | 5,000 | 2.10 |
| Gold-plated spring | 20-60 | 4/5 | 50,000 | 4.80 |
| Carbon nanotube composite | 8-25 | 4/5 | 20,000 | 7.50 |

### Key Findings

The carbon nanotube (CNT) composite electrodes consistently delivered the lowest impedance across diverse hair types and scalp conditions. However, the gold-plated spring contacts offer the best balance of comfort and longevity for a consumer product.

We need to cross-reference our impedance measurements with the [[Electrode Impedance QC Protocol]] used by the array manufacturing team, since their bench setup is our calibration reference.

### Decision

For the first prototype revision (P1.2), we will proceed with **gold-plated spring contacts** for temporal and occipital sites, and **CNT composite pads** for forehead channels (Fp1, Fp2) where hair is not a factor.

Material sourcing is underway. The CNT supplier requires a minimum order of 500 units, which aligns with our [[Beta Hardware Build Plan]] targets.

### Next Steps

- Finalize supplier contracts by end of Q2
- Run 72-hour wear tests with the selected materials
- Validate impedance stability over 8-hour sessions for [[SSVEP Frequency Tagging Protocol]]

#hardware #electrodes
`,
	},
	{
		Title:   "SSVEP Frequency Tagging Protocol",
		Project: 9,
		Tags:    []string{"paradigm", "ssvep", "signal-processing"},
		Body: `## SSVEP Frequency Tagging Protocol

### Background

Steady-State Visually Evoked Potentials are among the most robust BCI paradigms for non-invasive systems. We target four frequency bins for a basic command interface.

### Stimulus Frequencies

| Command | Frequency (Hz) | Harmonics Checked | Target Electrode |
|---------|----------------|-------------------|------------------|
| Up | 7.5 | 15.0, 22.5 | Oz |
| Down | 10.0 | 20.0, 30.0 | Oz |
| Left | 12.0 | 24.0, 36.0 | O1 |
| Right | 15.0 | 30.0, 45.0 | O2 |

### Signal Processing Chain

1. Bandpass filter: 5-45 Hz (4th order Butterworth)
2. Artifact rejection via amplitude threshold (+-100 uV)
3. CCA (Canonical Correlation Analysis) for frequency detection
4. Sliding window: 2 seconds with 0.5s step

    // Pseudocode for CCA-based detection
    func detectSSVEP(epoch []float64, fs float64, freqs []float64) int {
        maxCorr := 0.0
        bestIdx := -1
        for i, f := range freqs {
            refSignals := generateHarmonicRef(f, fs, len(epoch), 3)
            rho := canonicalCorrelation(epoch, refSignals)
            if rho > maxCorr {
                maxCorr = rho
                bestIdx = i
            }
        }
        return bestIdx
    }

### Integration Notes

The frequency detection feeds into the [[Low-Latency Intent Classification]] system used by the gaming team. Our 2-second window yields ~90% accuracy on bench tests, but real-world conditions with the [[Dry Electrode Material Selection]] contacts show 78-83%.

We are also evaluating whether the same pipeline can be adapted for the [[Phoneme Decoder Architecture]] project where spectral features are similarly critical.

### Open Issues

- LED flicker rate stability on the companion app needs validation
- Photosensitive epilepsy screening must be added per [[Phase I Trial Design]] requirements

#ssvep #signal-processing #paradigm
`,
	},
	{
		Title:   "P300 Speller Implementation",
		Project: 9,
		Tags:    []string{"paradigm", "p300", "signal-processing"},
		Body: `## P300 Speller Implementation

### Overview

The P300 paradigm serves as our secondary input method, complementing SSVEP for text entry scenarios. This note documents our adaptation of the classic row-column speller for the dry-electrode headset.

### Architecture

Our speller uses a 6x6 character matrix with row/column intensification at 5.7 Hz. The oddball detection pipeline runs on-device to minimize latency.

### Signal Processing

- Epoch window: -100 to 600 ms relative to stimulus onset
- Baseline correction using pre-stimulus interval
- Spatial filtering via xDAWN (3 components)
- Classification: shrinkage LDA trained per-user during 5-minute calibration

### Performance Benchmarks

| Metric | Lab Condition | Realistic (walking) |
|--------|---------------|---------------------|
| Single-trial accuracy | 72% | 54% |
| After 5 repetitions | 96% | 85% |
| Characters per minute | 4.8 | 2.1 |
| False positive rate | 3.2% | 11.7% |

The significant degradation during movement is expected. Motion artifacts from [[Dry Electrode Material Selection]] contacts are the primary culprit. We are investigating adaptive filtering approaches documented in [[Adaptive Noise Cancellation for EEG]].

### Calibration

The calibration procedure collects 30 target characters with 10 repetitions each. This aligns with the approach in [[Decoder Calibration Protocol]] but adapted for non-invasive signals with much lower SNR.

### Classifier Training

    # xDAWN + LDA pipeline
    xdawn = XdawnCovariances(nfilter=3)
    lda = MDM(metric="riemann")
    pipeline = make_pipeline(xdawn, lda)
    pipeline.fit(X_train, y_train)

### Integration

The speller component exposes a standard event interface compatible with the [[SDK Architecture Overview]]. User preferences and calibration data persist locally per the [[Neural Data Anonymization]] policy.

#p300 #paradigm
`,
	},
	{
		Title:   "Adaptive Noise Cancellation for EEG",
		Project: 9,
		Tags:    []string{"signal-processing", "noise", "algorithms"},
		Body: `## Adaptive Noise Cancellation for EEG

### Problem Statement

Consumer-grade dry-electrode EEG is plagued by several noise sources that clinical wet-electrode systems avoid. Our headset must handle these in firmware or the companion app processing layer.

### Noise Sources and Mitigation

| Noise Source | Frequency Range | Amplitude | Mitigation Strategy |
|-------------|-----------------|-----------|---------------------|
| 50/60 Hz powerline | Narrowband | 10-200 uV | Adaptive notch filter |
| EMG (facial/neck) | 20-300 Hz | 10-500 uV | ICA + thresholding |
| Eye blinks | 0.5-5 Hz | 50-500 uV | Regression via Fp1/Fp2 |
| Motion artifact | 0.1-10 Hz | 50-2000 uV | Accelerometer reference ANC |
| Electrode pop | Broadband impulse | >1000 uV | Transient detection + interpolation |

### Accelerometer-Referenced ANC

The headset includes a 3-axis IMU (BMI270) at the occipital mount point. We use a normalized LMS (NLMS) adaptive filter with the accelerometer channels as reference inputs.

    func nlmsUpdate(w []float64, x []float64, d float64, mu float64, eps float64) float64 {
        y := dot(w, x)
        e := d - y
        norm := dot(x, x) + eps
        for i := range w {
            w[i] += (mu / norm) * e * x[i]
        }
        return e
    }

Step size mu=0.01 was selected after sweep testing. Convergence requires approximately 2 seconds of data.

### Validation

We benchmarked the ANC pipeline against raw signals during a structured movement protocol (head turning, jaw clenching, walking). SNR improvement averaged 8.4 dB for motion artifacts.

These results directly impact the [[SSVEP Frequency Tagging Protocol]] and [[P300 Speller Implementation]] accuracy in mobile conditions. The noise floor also determines feasibility of the features described in [[Motor Imagery Classifier Design]].

The IMU data stream is also useful for the [[Real-Time Telemetry Dashboard]] team who want head-movement visualization.

#signal-processing #noise #algorithms
`,
	},
	{
		Title:   "Motor Imagery Classifier Design",
		Project: 9,
		Tags:    []string{"paradigm", "motor-imagery", "machine-learning"},
		Body: `## Motor Imagery Classifier Design

### Overview

Motor imagery (MI) provides a hands-free, gaze-free BCI paradigm. Users imagine left/right hand or foot movement. We detect event-related desynchronization (ERD) in the mu (8-12 Hz) and beta (18-26 Hz) bands over sensorimotor cortex.

### Electrode Selection

With our limited channel count (16 channels), we prioritize:
- C3, C4, Cz for primary motor cortex coverage
- FC3, FC4, CP3, CP4 for spatial filtering
- Remaining channels provide context for artifact rejection

### Feature Extraction Pipeline

1. Bandpass filter to mu (8-12 Hz) and beta (18-26 Hz)
2. Common Spatial Pattern (CSP) filter (3 pairs)
3. Log-variance of CSP-filtered signals as features
4. Optional: Riemannian geometry features on covariance matrices

### Classification Approaches Evaluated

| Method | 2-class Accuracy | 4-class Accuracy | Training Time |
|--------|-----------------|-----------------|---------------|
| CSP + LDA | 78.3% | 52.1% | <1s |
| CSP + SVM (RBF) | 80.1% | 55.8% | 3s |
| Riemannian MDM | 82.4% | 58.2% | 2s |
| EEGNet (deep learning) | 84.7% | 62.3% | 45min (GPU) |

For on-device inference, the Riemannian MDM approach gives the best accuracy-to-compute tradeoff. EEGNet is used for offline analysis and feeding into the [[Transformer Decoder Architecture]] research.

### Calibration Requirements

MI classifiers need more calibration data than SSVEP. Our protocol requires 40 trials per class (approximately 15 minutes). Transfer learning from a population model reduces this to 10 trials per class, following principles from [[Decoder Calibration Protocol]].

### Challenges

Dry electrodes over hair-covered sensorimotor cortex (C3/C4 region) yield higher impedance than occipital sites. The [[Dry Electrode Material Selection]] and [[Headset Mechanical Design]] directly constrain MI performance.

We plan to evaluate whether this classifier can feed into the [[Adaptive Difficulty Algorithm]] for neurorehab applications.

#motor-imagery #machine-learning
`,
	},
	{
		Title:   "Headset Mechanical Design",
		Project: 9,
		Tags:    []string{"hardware", "mechanical", "design"},
		Body: `## Headset Mechanical Design

### Design Constraints

The headset must balance electrode contact pressure, comfort for extended wear (target: 4 hours), and aesthetics acceptable for consumer use.

### Specifications

| Parameter | Target | Achieved (P1.1) |
|-----------|--------|------------------|
| Weight | <120 g | 142 g |
| Contact pressure per electrode | 1.5-3.0 N/cm2 | 1.2-4.1 N/cm2 |
| Adjustable head circumference | 52-62 cm | 54-60 cm |
| Max continuous wear | 4 hours | 2.5 hours (comfort limit) |
| IP rating | IPX2 (sweat) | Not tested |

### Structure

The frame uses a semi-rigid PEBA (Pebax) elastomer skeleton with 3D-printed electrode mounts at each of the 16 positions. The band follows the 10-20 system layout optimized for our channel selection.

### Electrode Pressure System

Each electrode mount uses a leaf-spring mechanism that provides consistent pressure across head sizes. Spring constants were tuned per region:

- Frontal (Fp1/Fp2): 0.8 N/mm -- minimal hair, low pressure needed
- Temporal (T7/T8): 1.2 N/mm -- moderate hair
- Parietal/Occipital (Pz/Oz/O1/O2): 1.5 N/mm -- dense hair regions

### Comfort Issues (P1.1)

User testing revealed hotspots at the temporal electrodes after 90 minutes. The pressure variance (1.2-4.1 N/cm2) exceeds our target range. Revision P1.2 will add silicone padding at contact points.

The electrode choice from [[Dry Electrode Material Selection]] and the comfort constraints jointly determine our achievable signal quality for [[SSVEP Frequency Tagging Protocol]] and [[Motor Imagery Classifier Design]].

### Manufacturing

We are coordinating with the [[Cleanroom Fabrication Process]] team for electrode component supply, though our assembly itself is standard PCB and injection molding.

### Next Revision Goals

- Reduce weight to <120 g by switching to carbon fiber reinforced headband
- Widen circumference range to 52-62 cm
- Add quick-release mechanism for individual electrode modules

#hardware #mechanical
`,
	},
	{
		Title:   "Firmware Architecture for EEG Headset",
		Project: 9,
		Tags:    []string{"firmware", "embedded", "architecture"},
		Body: `## Firmware Architecture for EEG Headset

### Platform

The headset runs on an nRF5340 SoC (dual-core ARM Cortex-M33). The application core handles signal processing and BLE communication. The network core manages the BLE stack.

### Module Layout

    Application Core (128 MHz):
    +-- main.c
    +-- adc_driver/       # ADS1299 SPI interface (8 channels x2)
    +-- dsp/              # Real-time filtering, artifact detection
    +-- ble_service/      # Custom GATT service for EEG streaming
    +-- imu_driver/       # BMI270 accelerometer/gyro
    +-- power_mgmt/       # Battery monitoring, sleep modes
    +-- flash_storage/    # Calibration data, config persistence

    Network Core (64 MHz):
    +-- BLE 5.3 SoftDevice (Nordic SDK)

### Data Flow

1. ADS1299 samples 16 channels at 250 Hz (32-bit per channel)
2. DMA transfers to ring buffer (2-second capacity)
3. DSP module applies bandpass filter, notch filter, and [[Adaptive Noise Cancellation for EEG]] stage
4. Downsampled and packaged into BLE packets (MTU 247 bytes)
5. Transmitted at ~20 ms intervals (50 Hz packet rate)

### BLE Data Format

Each packet contains:
- 4-byte timestamp (ms since boot)
- 1-byte sequence number
- N samples x 16 channels x 16-bit (after 32->16 bit compression)
- 1-byte IMU status flag

### Power Budget

| Component | Active (mA) | Sleep (uA) |
|-----------|-------------|------------|
| nRF5340 app core | 8.2 | 1.5 |
| ADS1299 x2 | 12.0 | 0 |
| BLE radio (tx) | 4.8 | 0.4 |
| IMU | 0.9 | 3.0 |
| **Total** | **25.9** | **4.9** |

With a 400 mAh battery, this yields approximately 15 hours of active use. This power profile is comparable to what the [[Wireless Transmitter Power Budget]] team is dealing with for implantable systems, though our constraints are less severe.

The firmware OTA update mechanism follows the same protocol being developed by the [[Implant OTA Update Protocol]] team.

#firmware #embedded
`,
	},
	{
		Title:   "Beta Hardware Build Plan",
		Project: 9,
		Tags:    []string{"planning", "hardware", "beta"},
		Body: `## Beta Hardware Build Plan

### Overview

This document outlines the plan for producing 50 beta units of the non-invasive EEG headset for external evaluation. Target ship date: Q3 2026.

### Bill of Materials (per unit)

| Component | Supplier | Unit Cost | Lead Time |
|-----------|----------|-----------|-----------|
| nRF5340 DK | Nordic/Mouser | $32.00 | 4 weeks |
| ADS1299 x2 | TI/Digikey | $48.00 | 6 weeks |
| Gold-plated spring electrodes x16 | Custom (Shenzhen) | $76.80 | 8 weeks |
| PEBA headband frame | Injection mold (local) | $18.50 | 10 weeks |
| Battery (400 mAh LiPo) | Adafruit | $7.95 | 2 weeks |
| PCB assembly | JLCPCB | $42.00 | 3 weeks |
| Packaging + accessories | Various | $15.00 | 2 weeks |
| **Total per unit** | | **$240.25** | |

### Build Schedule

- Week 1-2: Final PCB design review and Gerber submission
- Week 3-8: Component procurement (critical path: electrodes)
- Week 6-8: Injection mold tooling for headband
- Week 9-10: PCB assembly and bring-up testing
- Week 11-12: Full assembly and QA per [[Electrode Impedance QC Protocol]]
- Week 13: Firmware flash with [[Firmware Architecture for EEG Headset]] release candidate
- Week 14: Packaging and shipping

### Beta Program Integration

Units will be distributed through the [[Beta Program Rollout Plan]] managed by the SDK team. Each beta tester receives:
- 1x EEG headset
- 1x USB-C charging cable
- Quick start guide
- Access to beta SDK and companion app

### Risk Mitigation

The electrode supplier is single-source. We have identified a backup supplier (2x cost, 4-week lead time) in case of delays. The [[Headset Mechanical Design]] P1.2 revision must be finalized before mold tooling begins.

#planning #hardware #beta
`,
	},
	{
		Title:   "Companion App Signal Quality Display",
		Project: 9,
		Tags:    []string{"software", "mobile-app", "ux"},
		Body: `## Companion App Signal Quality Display

### Purpose

The companion mobile app must provide clear real-time feedback on signal quality so users can adjust headset fit without technical expertise. This is the single most important UX element for consumer adoption.

### Signal Quality Metrics

For each of the 16 channels, we compute:

1. **Contact impedance** -- measured via ADS1299 lead-off detection at startup
2. **Ongoing SNR** -- ratio of EEG band power (1-40 Hz) to high-frequency noise (60-125 Hz)
3. **Artifact rate** -- percentage of 1-second epochs exceeding amplitude threshold

### Quality Tiers

| Tier | Impedance | SNR | Display Color |
|------|-----------|-----|---------------|
| Excellent | <20 kOhm | >10 dB | Green |
| Good | 20-50 kOhm | 5-10 dB | Yellow |
| Poor | 50-100 kOhm | 0-5 dB | Orange |
| No contact | >100 kOhm | <0 dB | Red |

### Head Map Visualization

The app renders a top-down scalp map with color-coded electrode positions. Users see at a glance which electrodes need adjustment. This approach was inspired by the [[Real-Time Telemetry Dashboard]] used in the cloud platform, adapted for mobile.

### Guided Fit Procedure

When signal quality is below threshold on critical channels (Oz for [[SSVEP Frequency Tagging Protocol]], C3/C4 for [[Motor Imagery Classifier Design]]), the app walks the user through targeted adjustments:

1. Highlight the problem electrode on the map
2. Show a short animation of recommended adjustment
3. Re-measure impedance after adjustment
4. Repeat until all critical channels are in Good or Excellent tier

### Technical Implementation

The app receives raw quality metrics over BLE from the [[Firmware Architecture for EEG Headset]] custom GATT service. Rendering uses a lightweight WebGL canvas for the head map.

Data from fitting sessions is anonymized per [[Neural Data Anonymization]] before any analytics collection.

#software #mobile-app
`,
	},
	{
		Title:   "EEG Headset Regulatory Pathway",
		Project: 9,
		Tags:    []string{"regulatory", "planning"},
		Body: `## EEG Headset Regulatory Pathway

### Classification

As a non-invasive consumer EEG device marketed for wellness and general BCI interaction (not medical diagnosis), we target:

- **US**: FDA Class I exempt (EEG for non-diagnostic use). No 510(k) required if marketing claims exclude medical use.
- **EU**: MDR Class I measuring device under Rule 10. Requires CE marking and technical documentation.

### Regulatory Strategy

We are working with the [[FDA 510k Submission Timeline]] team to ensure our marketing language does not inadvertently trigger a higher classification. Key phrases to avoid:
- "Diagnose" or "detect" any medical condition
- "Treat" or "therapy" claims
- Reference to specific neurological disorders

### Required Documentation

| Document | Status | Owner |
|----------|--------|-------|
| Risk management file (ISO 14971) | In progress | Regulatory |
| Biocompatibility assessment | Pending [[Biocompatibility Testing Results]] | Materials |
| EMC testing (IEC 61326-1) | Not started | Hardware |
| Electrical safety (IEC 62368-1) | Not started | Hardware |
| Software lifecycle (IEC 62304) | In progress | Firmware |
| Technical documentation (EU MDR) | Not started | Regulatory |

### Key Dependencies

The [[EU MDR Classification]] analysis from the regulatory team will confirm our Class I assumption. If the device is classified higher due to measurement claims, the timeline extends by 6-12 months.

The [[Biocompatibility Testing Results]] are needed for any electrode materials that contact skin for extended periods. Our [[Dry Electrode Material Selection]] choices (gold-plated, CNT) both require ISO 10993 evaluation.

### Timeline

- Q2 2026: Complete risk management file
- Q3 2026: EMC and safety testing (during [[Beta Hardware Build Plan]])
- Q4 2026: Submit EU technical documentation
- Q1 2027: CE marking, begin consumer sales

#regulatory #planning
`,
	},
	{
		Title:   "Multi-Paradigm Hybrid BCI Control",
		Project: 9,
		Tags:    []string{"paradigm", "hybrid", "control"},
		Body: `## Multi-Paradigm Hybrid BCI Control

### Motivation

No single BCI paradigm achieves both high throughput and full flexibility with non-invasive dry electrodes. Our solution combines SSVEP, P300, and motor imagery into a unified control framework.

### Paradigm Strengths and Roles

| Paradigm | Strength | Assigned Role |
|----------|----------|---------------|
| SSVEP | Fast, high accuracy for discrete selection | Primary navigation (4 directions) |
| P300 | Good for character/item selection from grid | Text entry, menu selection |
| Motor Imagery | Hands/gaze free, continuous control | Mode switching, proportional control |

### Fusion Architecture

The hybrid controller maintains a finite state machine:

    States:
        NAVIGATION  -> SSVEP active, MI monitors for mode switch
        TEXT_ENTRY   -> P300 active, MI monitors for exit
        CONTINUOUS   -> MI active, SSVEP provides discrete overrides

    Transitions:
        NAVIGATION -> TEXT_ENTRY:  sustained left-hand MI (2s)
        TEXT_ENTRY -> NAVIGATION:  sustained right-hand MI (2s)
        NAVIGATION -> CONTINUOUS:  sustained bilateral MI (2s)
        CONTINUOUS -> NAVIGATION:  jaw clench artifact (intentional)

### Integration with External Systems

The state machine outputs standardized BCI commands that map to the [[Game Controller Abstraction Layer]] for gaming applications and the [[SDK Architecture Overview]] for third-party developers.

For neurorehab use cases, the CONTINUOUS mode feeds directly into the [[Motor Recovery Progress Tracking]] system which logs engagement metrics.

### Performance Target

Combined ITR (Information Transfer Rate) target: 25 bits/minute in NAVIGATION mode, 12 characters/minute in TEXT_ENTRY mode. These targets assume signal quality achievable with the [[Adaptive Noise Cancellation for EEG]] pipeline running.

### Open Research Questions

- Optimal transition thresholds to minimize false mode switches
- User fatigue during extended hybrid sessions
- Whether to allow simultaneous SSVEP + MI or strict alternation

#paradigm #hybrid #control
`,
	},
	{
		Title:   "Long-Term Wear Comfort Study Design",
		Project: 9,
		Tags:    []string{"user-study", "comfort", "design"},
		Body: `## Long-Term Wear Comfort Study Design

### Objective

Quantify subjective and objective comfort metrics for the EEG headset across 4-hour sessions to validate the [[Headset Mechanical Design]] P1.2 revision.

### Study Protocol

**Participants**: 20 healthy adults (10M/10F), age 22-55, varied hair types
**Sessions**: 3 sessions per participant, separated by at least 48 hours
**Duration**: 4 hours continuous wear with structured breaks

### Measurement Schedule

| Timepoint | Measurements |
|-----------|-------------|
| T0 (baseline) | Impedance, comfort VAS, headset pressure map |
| T30 | Impedance, comfort VAS |
| T60 | Impedance, comfort VAS, pressure map |
| T120 | Impedance, comfort VAS, SSVEP accuracy (5 min) |
| T180 | Impedance, comfort VAS, pressure map |
| T240 | Impedance, comfort VAS, pressure map, SSVEP accuracy (5 min) |

### Comfort Metrics

- Visual Analog Scale (0-100) for overall comfort, pressure points, heat
- Pressure mapping via Tekscan sensor array inserted at electrode-scalp interface
- Skin redness scoring (0-3 scale) at each electrode site post-removal
- Qualitative interview at session end

### Signal Quality Tracking

Impedance drift over 4 hours correlates with electrode-scalp coupling degradation. We will use the [[Companion App Signal Quality Display]] to log continuous quality metrics.

### Ethical Review

This study requires IRB approval. The protocol will follow guidelines compatible with [[Phase I Trial Design]] documentation, though this is a non-invasive device study with minimal risk.

Participant recruitment leverages the [[Participant Recruitment Strategy]] framework established by the clinical team.

### Analysis Plan

Primary outcome: percentage of participants maintaining "Good" or better signal quality on at least 12/16 channels at T240. Secondary: comfort VAS trend over time.

#user-study #comfort
`,
	},
	{
		Title:   "EEG Data Streaming Architecture",
		Project: 9,
		Tags:    []string{"software", "architecture", "streaming"},
		Body: `## EEG Data Streaming Architecture

### Overview

This note documents the end-to-end data path from headset ADC to application-layer processing. The architecture must support real-time BCI control with <100 ms total latency while also enabling session recording for offline analysis.

### Data Path

    [ADS1299 x2] --SPI--> [nRF5340] --BLE 5.3--> [Companion App]
                                                        |
                                            +-----------+-----------+
                                            |                       |
                                    [Real-time DSP]         [Session Recorder]
                                            |                       |
                                    [BCI Paradigm]          [Local Storage]
                                    [Classifiers]                   |
                                            |               [Cloud Upload]
                                    [Application]                   |
                                                          [[Multi-Site Data Sync Architecture]]

### BLE Throughput Budget

- 16 channels x 250 Hz x 16 bits = 64,000 bits/s = 8 KB/s raw EEG
- IMU data: 6 axes x 100 Hz x 16 bits = 1.2 KB/s
- Quality metrics: ~100 B/s
- **Total**: ~9.3 KB/s sustained

BLE 5.3 with 2M PHY and connection interval of 15 ms supports up to ~100 KB/s throughput, so we have comfortable margin.

### Session Recording Format

Sessions are stored in a custom binary format:

    Header (64 bytes):
        Magic: "SEAM" (4 bytes)
        Version: uint16
        Channel count: uint16
        Sample rate: uint32
        Start timestamp: int64 (Unix ns)
        Electrode montage ID: uint16

    Data blocks (variable):
        Block timestamp: int64
        Sample count: uint16
        Samples: [count][channels]int16
        IMU: [count][6]int16

### Privacy Considerations

All session recordings are encrypted at rest using keys derived from user credentials. The encryption scheme follows [[Encryption Protocol for Neural Streams]] guidelines. Session metadata is stripped of identifiers per [[Neural Data Anonymization]] before any aggregated analysis.

The streaming API is exposed through the [[REST API Specification]] for third-party applications.

#software #architecture
`,
	},
	{
		Title:   "Artifact Rejection Benchmark Results",
		Project: 9,
		Tags:    []string{"benchmarks", "signal-processing", "validation"},
		Body: `## Artifact Rejection Benchmark Results

### Objective

Quantify the performance of our artifact rejection pipeline against standard contaminated EEG datasets, comparing our approach to published methods.

### Test Datasets

| Dataset | Source | Artifacts Present | Duration |
|---------|--------|-------------------|----------|
| Internal-v1 | Lab collection (N=12) | EMG, blinks, motion | 48 hours |
| BNCI-2014-001 | Public benchmark | MI + eye artifacts | 18 hours |
| Synthetic-v3 | Generated | Calibrated noise injection | 100 hours |

### Pipeline Under Test

The full pipeline from [[Adaptive Noise Cancellation for EEG]]:
1. IMU-referenced NLMS for motion
2. EOG regression for blinks (using Fp1/Fp2)
3. Threshold-based epoch rejection (+-150 uV)
4. Optional: ICA for residual EMG

### Results (Internal-v1 Dataset)

| Method | Clean Epoch Recovery | SNR Improvement | Processing Time (1s epoch) |
|--------|---------------------|-----------------|---------------------------|
| Threshold only | 62.3% | 0 dB (baseline) | 0.1 ms |
| Threshold + EOG regression | 78.1% | 4.2 dB | 0.3 ms |
| Threshold + NLMS + EOG | 84.7% | 8.4 dB | 0.8 ms |
| Full pipeline (+ ICA) | 91.2% | 11.1 dB | 45 ms |

### Impact on BCI Accuracy

Applying the full pipeline to our [[SSVEP Frequency Tagging Protocol]] test data:
- Without artifact rejection: 71.2% accuracy
- With NLMS + EOG: 83.8% accuracy
- With full pipeline: 88.4% accuracy

For [[P300 Speller Implementation]], single-trial accuracy improved from 54% to 68% with the full pipeline.

### Computational Feasibility

The NLMS + EOG stages run comfortably on the nRF5340 application core (see [[Firmware Architecture for EEG Headset]]). ICA is too expensive for real-time on-device use and is reserved for offline analysis.

These benchmarks inform which pipeline configuration we recommend through the [[SDK Architecture Overview]] for different use cases.

The [[Spike Sorting Algorithm Comparison]] from the neural AI team uses a related approach for invasive signal cleanup, and we have exchanged filter coefficients.

#benchmarks #signal-processing
`,
	},
	{
		Title:   "Epidural Electrode Array Design",
		Project: 10,
		Tags:    []string{"hardware", "electrodes", "design"},
		Body: `## Epidural Electrode Array Design

### Overview

The spinal cord interface requires a custom epidural electrode array placed over the lumbosacral enlargement (L1-S1 vertebral levels) to target locomotor circuits. This note documents the array geometry and material specifications.

### Array Specifications

| Parameter | Value |
|-----------|-------|
| Total electrodes | 32 (4 columns x 8 rows) |
| Electrode diameter | 1.2 mm |
| Inter-electrode pitch | 3.0 mm (row), 2.5 mm (column) |
| Array dimensions | 24 mm x 10 mm |
| Substrate | Medical-grade silicone (50A durometer) |
| Electrode material | Platinum-iridium (90/10) |
| Lead wires | MP35N alloy, 50 um diameter |

### Anatomical Targeting

The array spans the dorsal rootlets from L2 to S2, covering the primary motor pools for:
- L2-L3: Hip flexors (iliopsoas)
- L3-L4: Knee extensors (quadriceps)
- L4-L5: Ankle dorsiflexors (tibialis anterior)
- S1-S2: Ankle plantarflexors (gastrocnemius/soleus)

### Fabrication

The array is manufactured using a modified version of the [[Cleanroom Fabrication Process]] with additional steps for the silicone substrate. Platinum-iridium contacts are sputter-deposited and patterned via photolithography.

Each array undergoes the [[Electrode Impedance QC Protocol]] with acceptance criteria of 500-2000 Ohms at 1 kHz in saline.

### Biocompatibility

All materials are ISO 10993 compliant. Long-term implant stability data from [[Biocompatibility Testing Results]] show <5% impedance drift over 12 months in the ovine model.

### Connection to Stimulator

The lead bundle terminates in a hermetic feedthrough connector compatible with our implantable pulse generator. The connector design must also support the data link requirements of the [[Implant Telemetry RF Link Design]].

#hardware #electrodes
`,
	},
	{
		Title:   "Stimulation Waveform Parameter Space",
		Project: 10,
		Tags:    []string{"stimulation", "parameters", "neuromodulation"},
		Body: `## Stimulation Waveform Parameter Space

### Overview

Effective spinal cord stimulation for locomotion requires careful tuning of stimulation parameters. This note defines the parameter space and our exploration strategy.

### Parameter Ranges

| Parameter | Min | Max | Step | Default |
|-----------|-----|-----|------|---------|
| Amplitude | 0.1 mA | 10.0 mA | 0.1 mA | 2.0 mA |
| Pulse width | 50 us | 1000 us | 10 us | 300 us |
| Frequency | 5 Hz | 120 Hz | 1 Hz | 40 Hz |
| Interphase gap | 10 us | 100 us | 10 us | 30 us |
| Burst count | 1 | 10 | 1 | 1 |
| Burst interval | 1 ms | 50 ms | 1 ms | N/A |

### Waveform Shape

We use charge-balanced biphasic pulses (cathodic-first) to prevent electrode corrosion and tissue damage:

    |--PW--|--IPG--|--PW--|
    |      |       |      |
    +------+       |      |
    |              +------+
    |                     |
    cathodic     anodic

### Spatial Patterns

The 32-electrode array supports multiple stimulation configurations:
- **Monopolar**: single active electrode, distant return
- **Bipolar**: adjacent electrode pairs
- **Multipolar**: weighted current steering across 3+ electrodes

Current steering allows virtual electrode positioning between physical contacts, critical for targeting specific dorsal rootlets without mechanical repositioning.

### Relationship to Other Work

Our parameter space overlaps significantly with the [[Micro-stimulation Parameter Space]] defined by the sensory feedback team. Key difference: we operate at much higher charge densities since epidural stimulation must penetrate cerebrospinal fluid and dura.

The optimization of these parameters will eventually be automated using approaches from [[Adaptive Difficulty Algorithm]], adapted for motor output rather than task difficulty.

### Safety Limits

Charge density must remain below 30 uC/cm2 per phase. With our 1.13 mm2 electrode area, this limits maximum charge per phase to 3.39 uC, corresponding to approximately 11.3 mA at 300 us pulse width.

#stimulation #parameters
`,
	},
	{
		Title:   "Gait Cycle Decoding from Residual EMG",
		Project: 10,
		Tags:    []string{"decoding", "emg", "gait"},
		Body: `## Gait Cycle Decoding from Residual EMG

### Objective

Many individuals with incomplete spinal cord injury retain residual volitional EMG activity below the lesion level. We decode this activity to infer intended gait phase and modulate stimulation accordingly.

### EMG Recording Setup

- 8 surface EMG channels on bilateral lower limb muscles:
  - Rectus femoris (RF), biceps femoris (BF)
  - Tibialis anterior (TA), medial gastrocnemius (MG)
- Sampling rate: 2000 Hz
- Bandpass: 20-450 Hz
- Notch: 50/60 Hz

### Feature Extraction

For each 50 ms window (updated every 25 ms):

    features = []
    for ch in channels:
        envelope = rms(ch, window=50ms)
        mav = mean_absolute_value(ch)
        zc = zero_crossings(ch, threshold=0.05mV)
        wl = waveform_length(ch)
        features.extend([envelope, mav, zc, wl])

This yields 32 features per window (4 features x 8 channels).

### Gait Phase Classification

We classify into 4 gait phases:
1. Loading response (0-12% cycle)
2. Mid-stance (12-50% cycle)
3. Pre-swing (50-62% cycle)
4. Swing (62-100% cycle)

| Classifier | Accuracy (complete SCI sim) | Accuracy (incomplete SCI) |
|------------|---------------------------|--------------------------|
| LDA | 71.2% | 82.4% |
| Random Forest | 76.8% | 87.1% |
| LSTM (50ms steps) | 81.3% | 91.7% |

The LSTM approach from [[Transformer Decoder Architecture]] research shows promise but latency is critical here. We use a lightweight 2-layer LSTM that runs in <5 ms on the implant processor.

### Closed-Loop Integration

Decoded gait phase triggers corresponding stimulation patterns from [[Stimulation Pattern Library for Locomotion]]. The decode-to-stimulation latency budget is 30 ms total. This is conceptually similar to the [[Closed-Loop Grasp Controller]] but for lower limb rhythmic movement.

#decoding #emg #gait
`,
	},
	{
		Title:   "Stimulation Pattern Library for Locomotion",
		Project: 10,
		Tags:    []string{"stimulation", "locomotion", "patterns"},
		Body: `## Stimulation Pattern Library for Locomotion

### Overview

This library contains pre-defined spatiotemporal stimulation patterns that activate locomotor muscle synergies via epidural stimulation. Patterns are triggered by gait phase decoded in [[Gait Cycle Decoding from Residual EMG]].

### Pattern Structure

Each pattern defines electrode activation across the 32-contact array for one gait phase:

    type StimPattern struct {
        Phase       GaitPhase
        Duration    time.Duration
        Electrodes  []ElectrodeConfig
    }

    type ElectrodeConfig struct {
        Index     int       // 0-31
        Amplitude float64   // mA
        PulseWidth int      // microseconds
        Delay     int       // ms offset from phase onset
    }

### Locomotor Patterns

**Loading Response** (heel strike to foot flat):
- Activate L3-L4 columns (knee extensors) at 40 Hz, 2.5 mA
- Co-activate S1 electrodes (plantarflexors) at 30 Hz, 1.8 mA
- Ramp onset over 50 ms

**Mid-Stance** (foot flat to heel off):
- Maintain L3-L4 at reduced amplitude (1.5 mA)
- Gradually increase S1-S2 (plantarflexors) to 3.0 mA
- Begin ramping down L2-L3 (hip flexors)

**Pre-Swing** (heel off to toe off):
- Peak S1-S2 activation (plantarflexors) for push-off
- Begin L2 activation (hip flexors) for swing initiation
- Parameters derived from [[Stimulation Waveform Parameter Space]]

**Swing** (toe off to heel strike):
- Activate L2-L3 (hip flexors) at 40 Hz, 2.0 mA
- Activate L4-L5 (dorsiflexors) at 35 Hz, 1.5 mA for foot clearance
- Inhibit S1-S2 (plantarflexors)

### Personalization

Baseline patterns are scaled per-participant based on motor threshold mapping during the [[Intraoperative Electrode Mapping Protocol]]. The [[Adaptive Difficulty Algorithm]] concepts are being adapted to progressively reduce stimulation amplitude as motor recovery progresses.

### Safety

All patterns enforce charge balance within each stimulation cycle and respect the safety limits in [[Stimulation Waveform Parameter Space]].

#stimulation #locomotion
`,
	},
	{
		Title:   "Intraoperative Electrode Mapping Protocol",
		Project: 10,
		Tags:    []string{"surgical", "mapping", "protocol"},
		Body: `## Intraoperative Electrode Mapping Protocol

### Purpose

During surgical placement of the [[Epidural Electrode Array Design]], we perform systematic mapping to identify optimal electrode positions for each target muscle group. This mapping informs personalized stimulation patterns.

### Pre-operative Planning

1. Review pre-op MRI to estimate vertebral level of conus medullaris
2. Plan array placement to center over lumbosacral enlargement
3. Prepare stimulation protocol with surgeon and neurophysiology team

### Mapping Procedure

Under stable anesthesia (propofol TIVA, no neuromuscular blockade):

1. Place array in initial position based on anatomical landmarks
2. For each electrode (1-32), deliver test pulses:
   - Start at 0.5 mA, 300 us, 2 Hz (single pulses)
   - Increment by 0.5 mA until motor response or 8 mA ceiling
   - Record threshold and recruited muscles via surface EMG

### Response Documentation

    Electrode | Threshold (mA) | Primary Muscle | Secondary | Laterality
    ---------|----------------|---------------|-----------|----------
    E01      | 1.5            | Quadriceps    | -         | Left
    E02      | 2.0            | Quadriceps    | Iliopsoas | Left
    E03      | 3.5            | Tib. Anterior | -         | Left
    ...

### Array Repositioning

If the mapping reveals poor coverage of critical muscle groups (hip flexors, dorsiflexors), the array is shifted rostral or caudal and remapped. Target: at least 2 electrodes per target muscle group per side.

### Data Integration

Mapping results feed directly into the [[Stimulation Pattern Library for Locomotion]] to set per-electrode amplitude scaling. Results are also shared with the [[ECoG Signal Preprocessing Pipeline]] team, as they use similar intraoperative mapping approaches for cortical arrays.

### Documentation Requirements

All mapping data is recorded for [[Adverse Event Reporting SOP]] compliance and included in the surgical report. The mapping protocol must receive IRB approval as part of the [[Phase I Trial Design]].

#surgical #mapping
`,
	},
	{
		Title:   "Closed-Loop Stimulation Controller",
		Project: 10,
		Tags:    []string{"control", "closed-loop", "real-time"},
		Body: `## Closed-Loop Stimulation Controller

### Architecture

The closed-loop controller integrates decoded motor intent from [[Gait Cycle Decoding from Residual EMG]] with biomechanical feedback to modulate stimulation parameters in real time.

### Control Loop

    +----------+      +-----------+      +------------------+
    | Residual | ---> |   Gait    | ---> |   Pattern        |
    |   EMG    |      |  Decoder  |      |   Selection      |
    +----------+      +-----------+      +------------------+
                                                |
    +----------+      +-----------+      +------v-----------+
    | IMU /    | ---> | Biomech   | ---> | Amplitude/Timing |
    | Gonio   |      | Estimator |      |   Modulation     |
    +----------+      +-----------+      +------------------+
                                                |
                                         +------v-----------+
                                         |   Stimulator     |
                                         |   Hardware       |
                                         +------------------+

### Control Update Rate

- EMG decode: every 25 ms (from [[Gait Cycle Decoding from Residual EMG]])
- Biomechanical state: every 10 ms (IMU at 100 Hz)
- Stimulation update: every 10 ms (100 Hz control rate)

### Modulation Rules

The controller adjusts stimulation based on biomechanical error signals:

| Feedback Signal | Error Condition | Adjustment |
|----------------|-----------------|------------|
| Knee angle (goniometer) | Insufficient extension in stance | Increase L3-L4 amplitude |
| Foot clearance (IMU) | Toe drag in swing | Increase L4-L5 amplitude |
| Trunk sway (IMU) | Lateral instability | Increase bilateral co-contraction |
| Step timing | Asymmetric cycle | Adjust phase durations |

### Safety Mechanisms

- Hardware current limiter: 10 mA per electrode
- Software charge density check every pulse
- Emergency stop on accelerometer fall detection
- Watchdog timer: if controller misses 3 consecutive updates, revert to open-loop tonic stimulation

### Comparison with Related Systems

This controller is architecturally similar to the [[Closed-Loop Grasp Controller]] developed by the sensory feedback team. Both use a predict-then-correct paradigm. Key difference: gait is rhythmic and benefits from central pattern generator (CPG) models, while grasp is discrete and force-regulated.

The controller state telemetry is transmitted via the [[Implant Telemetry RF Link Design]] for external monitoring.

#control #closed-loop
`,
	},
	{
		Title:   "Ovine Model Surgical Protocol",
		Project: 10,
		Tags:    []string{"preclinical", "surgical", "animal-model"},
		Body: `## Ovine Model Surgical Protocol

### Justification

The domestic sheep (Ovis aries) spinal cord anatomy at the lumbosacral level closely approximates human dimensions, making it the preferred large animal model for epidural spinal cord stimulation studies.

### Surgical Procedure

**Pre-operative**:
- 12-hour fast, pre-anesthetic bloodwork
- Anesthesia: isoflurane 1.5-2.5% in O2 after IV propofol induction
- Position: prone on radiolucent table
- Fluoroscopic confirmation of target vertebral levels

**Approach**:
1. Midline dorsal incision over L1-L5 spinous processes
2. Subperiosteal muscle dissection to expose laminae
3. Partial laminectomy at L2-L3 for array insertion
4. Advance [[Epidural Electrode Array Design]] rostrally under fluoroscopic guidance
5. Confirm final position with intraoperative mapping per [[Intraoperative Electrode Mapping Protocol]]

**Lead Routing**:
- Tunnel leads subcutaneously to lateral flank
- Connect to implantable pulse generator in subcutaneous pocket
- Verify all 32 channels with impedance check

**Closure**:
- Layered closure with absorbable sutures
- Subcuticular skin closure

### Post-operative Care

- 72-hour monitoring with analgesia (buprenorphine 0.01 mg/kg q8h)
- Impedance check at 24h, 72h, and weekly for 4 weeks
- Locomotor testing begins at 2 weeks post-op

### Regulatory

All procedures performed under IACUC protocol #2026-0142. Adverse events documented per [[Adverse Event Reporting SOP]] adapted for preclinical studies.

### Data Collection

Surgical and recovery data feed into the [[Training Data Pipeline]] for developing predictive models of electrode performance.

#preclinical #surgical
`,
	},
	{
		Title:   "Spinal Cord Computational Model",
		Project: 10,
		Tags:    []string{"modeling", "simulation", "computational"},
		Body: `## Spinal Cord Computational Model

### Purpose

A finite element model (FEM) of the lumbosacral spinal cord predicts current flow and neural activation for given electrode configurations, reducing the need for exhaustive in-vivo parameter sweeps.

### Model Components

| Tissue | Conductivity (S/m) | Source |
|--------|-------------------|--------|
| Cerebrospinal fluid | 1.70 | Literature |
| White matter (longitudinal) | 0.60 | Literature |
| White matter (transverse) | 0.08 | Literature |
| Gray matter | 0.23 | Literature |
| Epidural fat | 0.04 | Literature |
| Bone (vertebra) | 0.02 | Literature |
| Electrode (Pt-Ir) | 9.43e6 | Measured |

### Geometry

The model uses a subject-specific geometry derived from pre-operative MRI:
1. Segment spinal cord, CSF, vertebrae from T2-weighted MRI
2. Map dorsal rootlets from high-resolution diffusion tensor imaging
3. Mesh with tetrahedral elements (average edge length: 0.2 mm near electrodes)
4. Embed [[Epidural Electrode Array Design]] geometry on dura surface

### Simulation Pipeline

    MRI segmentation -> Meshing (Gmsh) -> FEM solve (FEniCS)
                                              |
                                    Voltage field -> NEURON models
                                              |
                                    Activation thresholds per rootlet

### Validation

Model predictions correlated with intraoperative mapping data ([[Intraoperative Electrode Mapping Protocol]]) at r=0.83 for motor threshold prediction across 6 ovine subjects.

### Applications

- Pre-operative planning: predict optimal array placement
- Virtual parameter sweeps: screen electrode configurations before testing in vivo
- Current steering optimization: find multi-electrode weightings for focused activation

The model outputs are used to pre-compute lookup tables for the [[Closed-Loop Stimulation Controller]], enabling real-time current steering without online FEM solves.

This computational approach shares methodology with the [[ECoG Signal Preprocessing Pipeline]] team who model cortical current spread.

#modeling #simulation
`,
	},
	{
		Title:   "Participant Screening and Inclusion Criteria",
		Project: 10,
		Tags:    []string{"clinical", "screening", "protocol"},
		Body: `## Participant Screening and Inclusion Criteria

### Target Population

Adults with chronic motor-complete or motor-incomplete spinal cord injury (SCI) at thoracic level, with preserved lumbosacral spinal circuitry below the lesion.

### Inclusion Criteria

1. Age 18-65 years
2. Chronic SCI (>12 months post-injury)
3. Neurological level of injury T4-T12 (AIS A, B, or C)
4. Stable neurological status for >6 months
5. No active infection, pressure ulcers, or uncontrolled spasticity
6. Adequate bone density for laminectomy (DEXA T-score > -2.5)
7. MRI-confirmed intact lumbosacral spinal cord below lesion
8. Able to provide informed consent and commit to 12-month study protocol

### Exclusion Criteria

- Pregnancy or planned pregnancy during study period
- Active implanted electrical device (cardiac pacemaker, other neurostimulator)
- History of autonomic dysreflexia requiring hospitalization
- Severe spasticity not controlled with oral medications
- Psychological condition that would impair study participation
- Contraindication to MRI (needed for pre-op planning)

### Screening Assessments

| Assessment | Purpose | Performed By |
|-----------|---------|-------------|
| Neurological exam (ISNCSCI) | Confirm AIS grade, level | Physiatrist |
| MRI lumbar spine | Verify cord integrity | Radiologist |
| DEXA scan | Bone density | Radiologist |
| EMG/NCS below lesion | Quantify residual activity | Neurophysiologist |
| Psychological screen | Readiness assessment | Psychologist |
| Autonomic testing | Dysreflexia risk | Physiatrist |

### Recruitment

We will recruit through the [[Participant Recruitment Strategy]] developed by the clinical trials program. Target enrollment: 10 participants for the first cohort.

All screening data is handled per [[HIPAA Compliance Checklist]] requirements. The full trial protocol is documented in [[Phase I Trial Design]].

#clinical #screening
`,
	},
	{
		Title:   "Locomotor Assessment Outcome Measures",
		Project: 10,
		Tags:    []string{"clinical", "outcomes", "assessment"},
		Body: `## Locomotor Assessment Outcome Measures

### Primary Outcome

**6-Minute Walk Test (6MWT)**: Distance walked in 6 minutes with stimulation ON vs. OFF. Minimum clinically important difference: 45 meters.

### Secondary Outcomes

| Measure | Description | Frequency |
|---------|-------------|-----------|
| 10-Meter Walk Test | Gait speed (m/s) | Weekly |
| Timed Up and Go | Functional mobility (seconds) | Weekly |
| WISCI II | Walking Index for SCI, 0-20 scale | Monthly |
| SCIM III | Spinal Cord Independence Measure | Monthly |
| Berg Balance Scale | Balance assessment, 0-56 | Monthly |
| EMG-based gait analysis | Muscle activation patterns | Bi-weekly |
| Kinematic analysis | Joint angles during gait | Bi-weekly |

### Instrumented Gait Analysis

Full 3D motion capture (Vicon) with synchronized EMG and stimulation logging:

    Data streams (time-synchronized):
    - 12 Vicon cameras @ 200 Hz
    - 16 surface EMG channels @ 2000 Hz
    - 3 force plates @ 1000 Hz
    - Stimulation parameter log @ 100 Hz
    - 2 IMUs (shank + thigh bilateral) @ 100 Hz

### Data Management

All outcome data is stored in the study database with participant IDs only (no names). Longitudinal data feeds into the [[Motor Recovery Progress Tracking]] system adapted from the neurorehab team.

Gait analysis data is also shared with the [[Training Data Pipeline]] for improving the [[Gait Cycle Decoding from Residual EMG]] classifiers.

### Assessment Schedule

- Baseline: 3 sessions over 2 weeks (pre-implant)
- Post-operative: weekly for first month, then bi-weekly
- Stimulation optimization: daily abbreviated assessments
- Long-term follow-up: monthly for 12 months

### Blinding

Assessors performing WISCI II and Berg Balance Scale are blinded to stimulation condition. The participant cannot be blinded due to the nature of the intervention. This follows the design outlined in [[Phase I Trial Design]].

#clinical #outcomes
`,
	},
	{
		Title:   "Spinal Interface Power Management",
		Project: 10,
		Tags:    []string{"hardware", "power", "implant"},
		Body: `## Spinal Interface Power Management

### Power Budget

The implantable pulse generator (IPG) must deliver stimulation across 32 channels while maintaining wireless communication and onboard processing.

| Component | Active Power (mW) | Duty Cycle | Average (mW) |
|-----------|--------------------|------------|---------------|
| Stimulation output stage | 120 | 40% | 48.0 |
| Microcontroller (ARM M4) | 35 | 100% | 35.0 |
| EMG AFE (8 channels) | 18 | 100% | 18.0 |
| Wireless telemetry | 45 | 10% | 4.5 |
| Housekeeping (temp, impedance) | 5 | 5% | 0.25 |
| **Total** | | | **105.75** |

### Battery

- Chemistry: Lithium-ion (medical grade)
- Capacity: 1200 mAh @ 3.7V = 4.44 Wh
- Target runtime: 16 hours per charge (therapy session + margin)
- Calculated runtime: 4440 / 105.75 = 42 hours (exceeds target)

### Charging

Wireless inductive charging via the [[Wireless Power Transfer Coil Design]] at 6.78 MHz. Target charge rate: 500 mA (full charge in ~3 hours).

### Power Optimization

The stimulation output stage dominates power consumption. Strategies:
- Use current recycling between adjacent bipolar pairs
- Adaptive supply voltage tracking (reduce compliance voltage to minimum needed)
- Sleep stimulation channels not in current gait phase

### Thermal Constraints

Implant surface temperature must not exceed 39 degrees C (2 degrees C above body temperature per ISO 14708-3). The [[Implant Thermal Management Strategy]] from the telemetry team provides guidelines. At our average power, FEM thermal modeling predicts a worst-case surface temperature rise of 0.8 degrees C, well within limits.

### Wireless Power Link

The power transfer efficiency and reliability are shared concerns with the [[Wireless Transmitter Power Budget]] analysis from the cortical decoder project.

#hardware #power
`,
	},
	{
		Title:   "Implant Telemetry RF Link Design",
		Project: 11,
		Tags:    []string{"rf", "wireless", "telemetry"},
		Body: `## Implant Telemetry RF Link Design

### Requirements

The telemetry link must support bidirectional data transfer between the implanted BCI device and an external relay unit worn on the body.

### Link Budget

| Parameter | Uplink (implant to external) | Downlink (external to implant) |
|-----------|------------------------------|-------------------------------|
| Data rate | 2 Mbps | 500 kbps |
| Range | 30 mm (through tissue) | 30 mm |
| Carrier frequency | 402-405 MHz (MICS band) | 402-405 MHz |
| TX power | -16 dBm (25 uW) | 0 dBm (1 mW) |
| Path loss (tissue) | -25 dB | -25 dB |
| Antenna gain (implant) | -15 dBi | -15 dBi |
| Antenna gain (external) | 0 dBi | 0 dBi |
| Received power | -56 dBm | -40 dBm |
| Receiver sensitivity | -90 dBm | -85 dBm |
| **Link margin** | **34 dB** | **45 dB** |

### Modulation

- Uplink: OQPSK with half-sine pulse shaping (spectral efficiency and low PAPR)
- Downlink: OOK for simplicity on the implant receiver

### Protocol Stack

    Application: Neural data packets / Commands
    Transport:   Sequence numbers, ACK/NACK, CRC-16
    MAC:         TDMA with configurable slot allocation
    PHY:         MICS band, OQPSK/OOK

### Antenna Design

The implant antenna is a meandered planar inverted-F antenna (PIFA) on the IPG lid, tuned for 403 MHz in a tissue-simulating environment. Simulation in CST Microwave Studio shows -18 dB return loss at center frequency.

### Integration Points

This RF link carries data for the [[Real-Time Telemetry Dashboard]] and supports the [[Implant OTA Update Protocol]] for firmware delivery. The power constraints are analyzed alongside the [[Wireless Transmitter Power Budget]] from the cortical decoder project.

Data transmitted over this link is encrypted per the [[Encryption Protocol for Neural Streams]] standard.

### Regulatory

The MICS band (402-405 MHz) is allocated for medical implant communications (FCC Part 95, ETSI EN 301 839). No licensing required.

#rf #wireless #telemetry
`,
	},
	{
		Title:   "Wireless Power Transfer Coil Design",
		Project: 11,
		Tags:    []string{"wireless-power", "hardware", "coils"},
		Body: `## Wireless Power Transfer Coil Design

### Overview

The implanted BCI device receives power wirelessly via near-field inductive coupling at 6.78 MHz (ISM band). This note documents the coil design and power transfer performance.

### Coil Specifications

| Parameter | Transmit Coil (external) | Receive Coil (implant) |
|-----------|--------------------------|------------------------|
| Diameter | 40 mm | 25 mm |
| Turns | 8 | 12 |
| Wire | Litz (48 AWG x 50 strands) | Litz (50 AWG x 25 strands) |
| Inductance | 3.2 uH | 4.8 uH |
| Q factor (unloaded) | 180 | 95 |
| Resonant frequency | 6.78 MHz | 6.78 MHz |

### Power Transfer Efficiency

Measured at 6.78 MHz with 10 mm tissue-equivalent gap:

| Alignment | Coupling (k) | Efficiency | Delivered Power |
|-----------|-------------|------------|-----------------|
| Coaxial, 10 mm gap | 0.35 | 78% | 850 mW |
| 5 mm lateral misalignment | 0.28 | 68% | 740 mW |
| 10 mm lateral misalignment | 0.18 | 51% | 555 mW |
| 15 mm lateral misalignment | 0.10 | 32% | 348 mW |

### Rectifier and Regulator

The implant-side power chain:

    RX Coil -> Series-parallel resonant match -> Full-bridge rectifier
                                                       |
                                               LDO regulator (3.3V)
                                                       |
                                               Battery charger IC
                                                       |
                                               Li-ion cell (1200 mAh)

### Alignment Feedback

The external coil unit includes an alignment indicator based on reflected impedance measurement. When coupling drops below k=0.15, the user is alerted to reposition. This feedback is displayed on the [[Real-Time Telemetry Dashboard]].

### Thermal Considerations

At 850 mW delivered with 78% efficiency, approximately 240 mW is dissipated in the coils and tissue. The [[Implant Thermal Management Strategy]] analysis confirms tissue temperature rise stays within 1.5 degrees C at this power level.

Power budget coordination with [[Wireless Transmitter Power Budget]] ensures the implant's total power budget is achievable.

#wireless-power #hardware
`,
	},
	{
		Title:   "Implant OTA Update Protocol",
		Project: 11,
		Tags:    []string{"firmware", "ota", "protocol"},
		Body: `## Implant OTA Update Protocol

### Motivation

Implanted devices require field-updatable firmware to address bugs, add features, and respond to safety findings without surgical explantation. This protocol defines a safe, reliable OTA update mechanism.

### Design Principles

1. **Atomic updates**: firmware is either fully applied or not at all (dual-bank A/B scheme)
2. **Cryptographic verification**: every image is signed with Ed25519 and verified before write
3. **Rollback capability**: previous firmware image retained for automatic fallback
4. **Power-safe**: update survives power loss at any point
5. **Bandwidth-efficient**: delta updates when possible

### Update Flow

    1. External controller announces update availability
    2. Implant confirms battery >50% and idle state
    3. Transfer image blocks via [[Implant Telemetry RF Link Design]]
       - Block size: 256 bytes
       - CRC-16 per block, retransmit on failure
       - Total image: ~128 KB typical (500 blocks)
    4. Verify SHA-256 hash of complete image
    5. Verify Ed25519 signature against embedded public key
    6. Write to inactive flash bank
    7. Set boot flag to new bank
    8. Reboot and verify (self-test within 5 seconds)
    9. If self-test fails, revert to previous bank

### Transfer Performance

| Metric | Value |
|--------|-------|
| RF data rate | 500 kbps downlink |
| Block size | 256 bytes |
| Blocks per second | ~180 (with overhead) |
| 128 KB image transfer | ~2.8 seconds |
| Verification time | ~0.5 seconds |
| Total update time | <10 seconds |

### Security

- Firmware images signed at build time with lab-held private key
- Public key fused into implant OTP memory at manufacturing
- Version rollback prevention via monotonic counter in OTP
- Related to broader security practices in [[Encryption Protocol for Neural Streams]]

### Integration

The OTA mechanism is triggered from the [[Multi-Site Data Sync Architecture]] cloud platform, which manages firmware version tracking across all deployed implants. The same update protocol is used for the EEG headset firmware described in [[Firmware Architecture for EEG Headset]].

#firmware #ota
`,
	},
	{
		Title:   "Implant Thermal Management Strategy",
		Project: 11,
		Tags:    []string{"thermal", "safety", "design"},
		Body: `## Implant Thermal Management Strategy

### Regulatory Requirement

Per ISO 14708-3 and FDA guidance for active implantable devices, the implant surface temperature must not exceed the surrounding tissue temperature by more than 2 degrees C under any operating condition.

### Heat Sources

| Source | Power Dissipation | Location |
|--------|-------------------|----------|
| Stimulation output | 15-48 mW | Output stage ICs |
| Microcontroller | 35 mW | SoC die |
| RF telemetry TX | 4.5 mW (avg) | PA die |
| Wireless power RX coil | 50-240 mW | Coil/rectifier |
| Battery charging | 30-80 mW | Charger IC |
| **Total (worst case)** | **401 mW** | |

### Thermal Model

A 3D finite element thermal model was constructed in COMSOL:

- Implant geometry: titanium enclosure, 45 x 30 x 8 mm
- Tissue layers: subcutaneous fat (5 mm), muscle, bone
- Blood perfusion: 0.5 mL/min/mL (muscle), 0.1 (fat)
- Boundary condition: core body temperature 37 degrees C

### Simulation Results

| Operating Mode | Peak Surface Temp Rise | Location |
|---------------|----------------------|----------|
| Stimulation only | 0.6 degrees C | Near output stage |
| Stimulation + charging | 1.4 degrees C | Near RX coil |
| Stimulation + OTA update | 0.8 degrees C | Near PA |
| Charging only (max rate) | 1.1 degrees C | Near RX coil |

All conditions stay within the 2 degrees C limit. The worst case occurs during simultaneous stimulation and wireless charging.

### Thermal Protection

The firmware implements a thermal watchdog:
1. On-chip temperature sensor sampled every 100 ms
2. If T > 38.5 degrees C: reduce charging current by 50%
3. If T > 39.0 degrees C: pause charging, reduce stimulation
4. If T > 39.5 degrees C: emergency shutdown, alert external controller

### Design Validation

Thermal testing follows the protocol in [[Biocompatibility Testing Results]] for chronic implant temperature monitoring in the ovine model.

Results feed into the [[FDA 510k Submission Timeline]] documentation package.

#thermal #safety
`,
	},
	{
		Title:   "Telemetry Data Packet Format",
		Project: 11,
		Tags:    []string{"protocol", "data-format", "telemetry"},
		Body: `## Telemetry Data Packet Format

### Overview

This note defines the binary packet format used for neural data uplink from implant to external controller via the [[Implant Telemetry RF Link Design]].

### Packet Structure

    Offset  Size    Field
    0       1       Sync byte (0xA5)
    1       1       Packet type
    2       2       Sequence number (uint16, big-endian)
    4       4       Timestamp (uint32, microseconds since boot)
    8       2       Payload length (uint16, big-endian)
    10      N       Payload (variable, type-dependent)
    10+N    2       CRC-16 (CCITT)

### Packet Types

| Type ID | Name | Payload | Rate |
|---------|------|---------|------|
| 0x01 | Neural data | Compressed samples | 50 Hz |
| 0x02 | Impedance | 32x uint16 (Ohms) | 0.1 Hz |
| 0x03 | Stimulation log | Active config snapshot | 10 Hz |
| 0x04 | Device status | Battery, temp, errors | 1 Hz |
| 0x05 | EMG data | 8-ch compressed samples | 50 Hz |
| 0x06 | IMU data | 6-axis, 16-bit | 20 Hz |
| 0xFF | OTA data block | Firmware chunk | On demand |

### Neural Data Payload (Type 0x01)

    Offset  Size    Field
    0       1       Channel mask (bitmask of active channels)
    1       1       Compression flags
    2       2       Sample count per channel
    4       N       Compressed sample data (delta-RLE encoded)

### Compression

Delta-RLE encoding achieves approximately 3:1 compression on typical neural data:
1. Compute first-order differences between consecutive samples
2. Run-length encode repeated delta values
3. Variable-length integer encoding for deltas

### Bandwidth Utilization

    Neural (96ch, 30kHz): ~800 kbps compressed
    EMG (8ch, 2kHz):      ~50 kbps compressed
    Overhead + other:      ~100 kbps
    Total:                 ~950 kbps (within 2 Mbps uplink capacity)

### Downstream Processing

Packets are decompressed by the external controller and forwarded to the [[Real-Time Telemetry Dashboard]] via WebSocket. The packet format is also documented in the [[REST API Specification]] for third-party integration.

Data privacy controls apply per [[HIPAA Compliance Checklist]] -- no patient identifiers appear in telemetry packets.

#protocol #data-format
`,
	},
	{
		Title:   "External Relay Unit Hardware",
		Project: 11,
		Tags:    []string{"hardware", "relay", "external"},
		Body: `## External Relay Unit Hardware

### Purpose

The external relay unit (ERU) bridges the implanted device and the outside world. It handles wireless power delivery, MICS-band telemetry, and Bluetooth connectivity to phones/tablets.

### Block Diagram

    [Implant] <-- MICS RF --> [ERU MICS Radio]
    [Implant] <-- 6.78 MHz --> [ERU Power TX Coil]

    ERU Internal:
    +-- MICS transceiver (CC1200)
    +-- Power amplifier (6.78 MHz, Class E)
    +-- nRF5340 (BLE + processing)
    +-- Alignment sensor (reflected impedance)
    +-- Battery (2000 mAh LiPo)
    +-- USB-C (charging + debug)
    +-- Status LEDs (3x)

### Specifications

| Parameter | Value |
|-----------|-------|
| Dimensions | 65 x 45 x 12 mm |
| Weight | 48 g (with battery) |
| Battery life | 8 hours (continuous telemetry + charging) |
| BLE range | 10 m (to phone/tablet) |
| Attachment | Adhesive patch or clip-on |
| Charging | USB-C, 1A |
| Operating temp | 15-40 degrees C |

### Firmware Architecture

The ERU firmware on the nRF5340 manages:
1. MICS radio scheduling (TDMA sync with implant)
2. Power TX regulation (closed-loop via implant feedback)
3. BLE GATT services for data streaming to app
4. Packet routing: implant telemetry -> BLE -> app
5. OTA relay: app -> BLE -> MICS -> implant (per [[Implant OTA Update Protocol]])

### Coil Integration

The TX power coil ([[Wireless Power Transfer Coil Design]]) is mounted on the skin-facing side of the ERU with a flexible adhesive gasket for comfort during extended wear.

### Data Path to Cloud

    ERU -> BLE -> Phone App -> HTTPS -> [[Multi-Site Data Sync Architecture]]

All data in transit is encrypted. The ERU does not store any neural data persistently -- it is a pass-through device. Encryption requirements follow [[Encryption Protocol for Neural Streams]].

#hardware #relay
`,
	},
	{
		Title:   "Implant Antenna Simulation and Tuning",
		Project: 11,
		Tags:    []string{"rf", "antenna", "simulation"},
		Body: `## Implant Antenna Simulation and Tuning

### Challenge

Designing an antenna for a device implanted under tissue presents unique challenges: lossy propagation medium, severe size constraints, and detuning from tissue contact and body movement.

### Antenna Topology

We selected a meandered planar inverted-F antenna (PIFA) integrated on the titanium enclosure lid of the IPG:

- Radiating element: copper trace on LTCC substrate
- Ground plane: titanium enclosure body
- Feed: coaxial via through ceramic feedthrough
- Dimensions: 18 x 8 mm (fits on IPG lid)

### Simulation Environment

- Tool: CST Microwave Studio 2025
- Tissue model: 4-layer (skin 2mm, fat 5mm, muscle 30mm, bone)
- Dielectric properties at 403 MHz from IT'IS database

### Results

| Parameter | In Air | In Tissue Model |
|-----------|--------|-----------------|
| Resonant frequency | 418 MHz | 403 MHz |
| Return loss (S11) | -22 dB | -18 dB |
| Bandwidth (-10 dB) | 12 MHz | 8 MHz |
| Radiation efficiency | 62% | 3.1% |
| Peak gain | -2.1 dBi | -15.2 dBi |

The 3.1% radiation efficiency in tissue is expected and consistent with literature for MICS-band implant antennas. The link budget in [[Implant Telemetry RF Link Design]] accounts for this with generous margin.

### Tuning Methodology

The antenna was designed in-air then detuned to resonance in tissue:
1. Initial design targets 418 MHz in free space (15 MHz above MICS)
2. Tissue loading shifts resonance down by approximately 15 MHz
3. Fine-tune meander length in simulation with tissue model
4. Validate on phantom (tissue-simulating liquid, sigma=0.8 S/m)

### Manufacturing Tolerance

Simulated sensitivity to dimensional variation:
- +/- 0.5 mm meander length: +/- 3 MHz frequency shift
- Tissue thickness variation +/- 2 mm: +/- 2 MHz shift
- Total: within 8 MHz bandwidth, robust to expected variation

### Phantom Validation

Measured results on the tissue phantom agree with simulation within 1.5 dB for gain and 2 MHz for resonant frequency. Full validation results documented for the [[FDA 510k Submission Timeline]] submission package.

#rf #antenna
`,
	},
	{
		Title:   "Implant Firmware Architecture",
		Project: 11,
		Tags:    []string{"firmware", "architecture", "embedded"},
		Body: `## Implant Firmware Architecture

### Platform

The implant runs on an ARM Cortex-M4F (STM32L4R9) chosen for its low-power modes and DSP capabilities. Real-time constraints require a bare-metal scheduler (no full RTOS overhead).

### Module Layout

    Core:
    +-- main.c              # Initialization, scheduler
    +-- scheduler.c         # Cooperative round-robin with priorities
    +-- watchdog.c          # Hardware + software watchdog

    Drivers:
    +-- adc_driver/         # Neural + EMG ADC interfaces
    +-- stim_driver/        # DAC + current source control
    +-- rf_driver/          # CC1200 MICS transceiver
    +-- wpt_driver/         # Wireless power monitoring
    +-- flash_driver/       # Dual-bank NOR flash
    +-- temp_sensor/        # On-die + board thermistor

    Application:
    +-- telemetry/          # Packet formation, compression
    +-- stim_engine/        # Pattern playback, safety checks
    +-- decoder/            # EMG/neural decode (lightweight LSTM)
    +-- ota/                # Over-the-air update manager
    +-- safety/             # Thermal, charge density, watchdog

### Task Schedule

    Priority | Task           | Period  | WCET
    --------|----------------|---------|------
    1       | Stim safety    | 100 us  | 15 us
    2       | ADC readout    | 33 us   | 20 us
    3       | DSP/decode     | 25 ms   | 8 ms
    4       | Telemetry TX   | 20 ms   | 3 ms
    5       | Housekeeping   | 1 s     | 5 ms
    6       | OTA handler    | On-demand| 50 ms

### Memory Map

- Flash Bank A: 512 KB (active firmware)
- Flash Bank B: 512 KB (backup/OTA staging, per [[Implant OTA Update Protocol]])
- SRAM: 640 KB (256 KB neural buffer, 128 KB stim patterns, 256 KB general)
- OTP: 1 KB (device ID, crypto keys, monotonic counter)

### Safety Architecture

Dual-redundant safety checks run on every stimulation pulse:
1. Software check: charge density calculation in stim_engine
2. Hardware check: independent comparator on output current

If either check fails, the hardware interlock disables the output stage within 10 us. This architecture follows recommendations for the [[FDA 510k Submission Timeline]] active implant guidance.

### Power Management

Sleep modes follow the state machine in [[Spinal Interface Power Management]]. The firmware aggressively gates unused peripherals, achieving <50 uA in standby.

#firmware #architecture
`,
	},
	{
		Title:   "Multi-Device Synchronization Protocol",
		Project: 11,
		Tags:    []string{"protocol", "synchronization", "multi-device"},
		Body: `## Multi-Device Synchronization Protocol

### Problem

A patient may have multiple implanted devices (e.g., cortical recorder + spinal stimulator) that need time-synchronized operation. The telemetry system must coordinate data streams from multiple implants.

### Synchronization Architecture

The external relay unit ([[External Relay Unit Hardware]]) serves as the timing master. All implants synchronize to its TDMA frame structure.

### TDMA Frame

    Frame period: 20 ms
    |-- Slot 0 (2ms) --| Slot 1 (2ms) | ... | Slot 7 (2ms) | Guard (4ms) |
    |   Beacon/Sync    |  Device 1 UL  | ... | Device 7 UL  | DL + idle   |

- Slot 0: ERU transmits beacon with frame counter and UTC timestamp
- Slots 1-7: allocated to implanted devices for uplink
- Guard: downlink commands and idle time

### Clock Synchronization

Each implant has a 32.768 kHz crystal (+-20 ppm). Over a 20 ms frame:
- Maximum drift: 0.4 us (well within slot guard bands of 100 us)
- Sync correction applied every frame based on beacon timestamp
- Long-term accuracy: <10 us after initial sync

### Multi-Implant Data Fusion

When cortical and spinal devices are synchronized, the [[Real-Time Telemetry Dashboard]] can display aligned neural and EMG/stimulation data. This enables:

- Correlating cortical intent signals with spinal stimulation delivery
- Measuring cortical-spinal loop latency
- Closed-loop systems spanning cortical decode and spinal output

### Bandwidth Allocation

| Device | Data Rate | Slots Needed |
|--------|-----------|-------------|
| Cortical recorder (96ch, 30kHz) | 800 kbps | 4 |
| Spinal stimulator (8ch EMG + stim log) | 150 kbps | 1 |
| Reserved | - | 2 |

### Compatibility

The protocol is designed to be forward-compatible with the [[SDK Architecture Overview]] so that third-party devices could join the synchronized network in future.

This multi-device scenario is particularly relevant for the clinical configurations described in [[Phase I Trial Design]].

#protocol #synchronization
`,
	},
	{
		Title:   "EMC Testing and Interference Mitigation",
		Project: 11,
		Tags:    []string{"emc", "testing", "regulatory"},
		Body: `## EMC Testing and Interference Mitigation

### Regulatory Requirements

Active implantable medical devices must comply with:
- IEC 60601-1-2 (EMC for medical electrical equipment)
- FDA guidance on EMC for implants
- MRI conditional labeling (if applicable)

### Test Categories

| Test | Standard | Limit | Status |
|------|----------|-------|--------|
| Radiated emissions | CISPR 11 Class B | Per frequency band | Planned Q3 |
| Conducted emissions | CISPR 11 | Per frequency band | Planned Q3 |
| Radiated immunity | IEC 61000-4-3 | 10 V/m, 80-2700 MHz | Planned Q3 |
| ESD immunity | IEC 61000-4-2 | 8 kV contact, 15 kV air | Planned Q3 |
| MRI compatibility | ASTM F2052, F2213 | 1.5T and 3T | Planned Q4 |

### Known Interference Sources

The telemetry link at 403 MHz ([[Implant Telemetry RF Link Design]]) could be susceptible to:
- Cell phones (700-2600 MHz): low risk, out of band
- WiFi (2.4/5 GHz): low risk, out of band
- Metal detectors (1-100 kHz): moderate risk, may trigger false alarms
- MRI (64/128 MHz + gradients): high risk, requires specific mitigations

### MRI Conditional Design

To achieve MRI conditional labeling:
1. All leads routed with minimal loop area to reduce RF heating
2. Electrode-tissue interface impedance monitored during scan
3. Device enters MRI-safe mode: stimulation off, telemetry off, low-power monitoring
4. The [[Implant Firmware Architecture]] includes an MRI detection feature (gradient field sensing)

### Pre-compliance Testing

Before formal testing, we perform pre-compliance scans in our shielded room:
- Near-field probe scan of ERU and implant
- Susceptibility sweep with signal generator + amplifier
- Results guide PCB layout revisions

### Documentation

All EMC test results feed into the technical file for [[EU MDR Classification]] and [[FDA 510k Submission Timeline]]. The testing schedule aligns with the [[Beta Hardware Build Plan]] for the external components.

#emc #testing
`,
	},
	{
		Title:   "Telemetry Link Reliability Analysis",
		Project: 11,
		Tags:    []string{"reliability", "analysis", "telemetry"},
		Body: `## Telemetry Link Reliability Analysis

### Objective

Quantify the packet error rate (PER) and link availability for the implant-to-ERU telemetry channel under realistic operating conditions.

### Test Setup

- Implant prototype in tissue phantom (30 mm depth)
- ERU mounted on phantom surface with standard adhesive
- Ambient RF environment: office building (WiFi, BLE, cell signals)
- Test duration: 72 hours continuous

### Results Summary

| Metric | Value |
|--------|-------|
| Total packets transmitted | 12,960,000 |
| Packets received correctly | 12,908,736 |
| Packet error rate (raw) | 0.40% |
| Packet error rate (after retransmit) | 0.003% |
| Max consecutive errors | 12 (0.24 s gap) |
| Link availability | 99.997% |
| Mean time between errors | 4.2 seconds |

### Error Analysis

| Error Cause | Percentage of Errors |
|-------------|---------------------|
| CRC failure (bit errors) | 67% |
| Missed slot (timing drift) | 18% |
| Collision with external RF | 11% |
| Unknown | 4% |

### Impact on Clinical Operation

The 0.003% post-retransmit PER means approximately one lost packet per 33,000 packets, or roughly once every 11 minutes. For the [[Closed-Loop Stimulation Controller]], lost packets are handled by holding the last valid stimulation state.

For the [[Real-Time Telemetry Dashboard]], occasional gaps are interpolated in the display. The dashboard team confirmed this PER is acceptable for their visualization requirements.

### Worst-Case Scenario

During deliberate jamming at 403 MHz (worst-case interference test), the link maintained connectivity with 2.1% PER post-retransmit by frequency hopping across the 3 MHz MICS band. This exceeds the threshold for safe closed-loop operation, so the system correctly fell back to open-loop tonic stimulation.

### Comparison to Requirements

The reliability exceeds our design target of 99.99% availability, providing confidence for the [[Phase I Trial Design]] clinical protocol.

#reliability #analysis
`,
	},
	{
		Title:   "Power Amplifier Design for Wireless Charging",
		Project: 11,
		Tags:    []string{"hardware", "power", "rf-design"},
		Body: `## Power Amplifier Design for Wireless Charging

### Overview

The external relay unit requires a 6.78 MHz power amplifier to drive the TX coil of the [[Wireless Power Transfer Coil Design]] for wireless charging of the implant battery.

### Topology Selection

We evaluated three PA topologies:

| Topology | Efficiency | Complexity | Output Power | Selected |
|----------|------------|------------|-------------|----------|
| Class D | 85-90% | Medium | 1-5 W | No |
| Class E | 90-95% | Low | 0.5-2 W | Yes |
| Class EF2 | 92-96% | High | 1-10 W | No |

Class E was selected for its simplicity and high efficiency at our target power level (1.5 W).

### Class E Design

    VDD (5V) ---[RFC]---+---[L_shunt]---+
                        |               |
                    [MOSFET]        [C_shunt]
                        |               |
                       GND             GND
                        
    Output: MOSFET drain -> series C -> TX coil (tuned to 6.78 MHz)

### Component Values

| Component | Value | Part |
|-----------|-------|------|
| MOSFET | 25 mOhm Rds(on) | EPC2036 (GaN) |
| RFC | 10 uH | Coilcraft XAL6060 |
| C_shunt | 330 pF | C0G ceramic |
| C_series | 180 pF | C0G ceramic |
| Gate driver | 4A peak | LM5114 |

### Measured Performance

| Metric | Value |
|--------|-------|
| Input power (DC) | 1.65 W |
| Output power (RF) | 1.50 W |
| PA efficiency | 91.2% |
| Harmonic suppression (2nd) | -38 dBc |
| Harmonic suppression (3rd) | -42 dBc |

### Thermal

The GaN FET dissipates ~150 mW at full power. With the ERU thermal pad and plastic enclosure, junction temperature stays below 65 degrees C. This is well within the operating range, unlike the tighter constraints of the [[Implant Thermal Management Strategy]].

### Power Control

Output power is regulated via supply voltage modulation (buck converter before PA). The control loop maintains constant delivered power to the implant despite coil misalignment, using feedback from the implant via [[Implant Telemetry RF Link Design]].

Total ERU power budget coordinates with [[Wireless Transmitter Power Budget]] analysis.

#hardware #power
`,
	},
	{
		Title:   "Implant Enclosure and Hermetic Sealing",
		Project: 11,
		Tags:    []string{"hardware", "enclosure", "manufacturing"},
		Body: `## Implant Enclosure and Hermetic Sealing

### Requirements

The implant enclosure must maintain hermeticity for the device lifetime (target: 10 years) while accommodating the RF antenna, power receiving coil, and electrical feedthroughs.

### Enclosure Design

| Parameter | Value |
|-----------|-------|
| Material | Grade 2 titanium |
| Dimensions | 45 x 30 x 8 mm |
| Wall thickness | 0.5 mm |
| Weight (empty) | 12 g |
| Internal volume | 8.2 cm3 |

### Hermetic Seal

The enclosure uses a laser-welded titanium lid with:
- Continuous seam weld (Nd:YAG laser, 15W, 2 mm/s)
- Helium leak rate specification: <1e-9 atm*cc/s
- Weld penetration: 0.3 mm (60% of wall thickness)

### Feedthroughs

| Feedthrough | Count | Type |
|-------------|-------|------|
| Stimulation electrodes | 32 | Alumina ceramic, Pt-Ir pins |
| EMG inputs | 8 | Alumina ceramic, Pt pins |
| RF antenna | 1 | Sapphire window (MICS) |
| Power coil | 1 | Ferrite-core hermetic |

The 32-pin electrode feedthrough is the most critical component. Each pin must maintain >100 MOhm isolation at 85 degrees C, 85% RH after 1000 hours. Manufacturing follows the [[Cleanroom Fabrication Process]] with additional hermetic sealing steps.

### Assembly Sequence

1. Populate PCB with components ([[Implant Firmware Architecture]] verified)
2. Wire-bond feedthrough pins to PCB pads
3. Insert battery and connect
4. Functional test in dry nitrogen atmosphere
5. Laser weld lid in dry nitrogen glove box
6. Helium leak test
7. Final functional test
8. Biocompatibility surface treatment ([[Biocompatibility Testing Results]])

### Quality Control

100% helium leak test. Any unit exceeding 1e-9 atm*cc/s is rejected. Historical yield: 94% at this specification. Failed units are destructively analyzed to improve the welding process.

### Sterilization Compatibility

The sealed enclosure withstands ethylene oxide (EtO) sterilization. Gamma irradiation is not recommended due to potential effects on the embedded lithium-ion battery.

Documentation for [[FDA 510k Submission Timeline]] includes full material traceability and process validation records.

#hardware #enclosure
`,
	},
	{
		Title:   "Telemetry Security Architecture",
		Project: 11,
		Tags:    []string{"security", "encryption", "architecture"},
		Body: `## Telemetry Security Architecture

### Threat Model

Implanted BCI devices face unique security threats:
1. **Eavesdropping**: Unauthorized interception of neural data
2. **Injection**: Malicious stimulation commands
3. **Replay**: Re-transmission of captured valid commands
4. **Denial of service**: Jamming the telemetry link
5. **Firmware tampering**: Unauthorized firmware modification

### Security Layers

| Layer | Mechanism | Protection Against |
|-------|-----------|-------------------|
| Physical | Short-range MICS (30 mm) | Eavesdropping (partially) |
| Transport | AES-128-CCM encryption | Eavesdropping, injection |
| Authentication | ECDH key exchange + mutual auth | Impersonation |
| Integrity | CMAC per packet | Injection, replay |
| Anti-replay | Monotonic sequence counter | Replay attacks |
| Firmware | Ed25519 signature verification | Firmware tampering |

### Key Management

    Initial pairing (in clinic, short-range):
    1. Implant generates ECDH key pair (Curve25519)
    2. Public key transmitted to ERU via NFC touch
    3. ECDH shared secret derived
    4. AES-128 session key derived from shared secret via HKDF
    5. Session key rotated every 24 hours

### Encryption Overhead

| Metric | Without Encryption | With AES-128-CCM |
|--------|-------------------|-------------------|
| Packet overhead | 0 bytes | 12 bytes (nonce + tag) |
| Processing time per packet | - | 45 us (hardware AES) |
| Throughput reduction | - | <2% |

The hardware AES accelerator in the STM32L4 makes encryption overhead negligible. This implementation follows the [[Encryption Protocol for Neural Streams]] standard defined by the data security team.

### Incident Response

If a key compromise is suspected:
1. Revoke session keys via clinic-initiated re-pairing
2. Audit telemetry logs on [[Real-Time Telemetry Dashboard]]
3. Report per [[Adverse Event Reporting SOP]] if patient safety affected
4. Review against [[HIPAA Compliance Checklist]] for data breach notification requirements

### Compliance

The security architecture has been reviewed against [[Neural Data Anonymization]] requirements and FDA cybersecurity guidance for medical devices.

#security #encryption
`,
	},
	{
		Title:   "Phoneme Classification Architecture",
		Project: 12,
		Tags:    []string{"decoding", "phoneme", "architecture"},
		Body: `## Phoneme Classification Architecture

Our speech prosthesis decoding pipeline classifies attempted phonemes from motor cortex activity recorded via high-density ECoG arrays. The current architecture uses a hierarchical approach:

### Pipeline Stages

1. **Signal Acquisition** -- 256-channel ECoG at 30 kHz, downsampled to 1 kHz after bandpass filtering (see [[ECoG Signal Preprocessing Pipeline]])
2. **Feature Extraction** -- High-gamma envelope (70-150 Hz) computed in 50 ms windows with 10 ms stride
3. **Phoneme Classifier** -- Temporal convolutional network (TCN) mapping feature windows to 39 ARPAbet phonemes
4. **Language Model Rescoring** -- Beam search with GPT-2 based LM to resolve ambiguous phoneme sequences

### Performance Summary

| Metric | Value | Target |
|--------|-------|--------|
| Top-1 phoneme accuracy | 74.3% | 80% |
| Top-3 phoneme accuracy | 91.2% | 95% |
| Latency (feature + classify) | 38 ms | < 50 ms |
| Vocabulary (with LM) | 1,200 words | 5,000 words |

The TCN consists of 6 dilated causal convolution blocks with residual connections. We chose TCN over LSTM after benchmarking showed 2.1x faster inference with comparable accuracy. The [[Transformer Decoder Architecture]] from the Neural Signal AI team is being evaluated as a potential replacement for the TCN stage.

### Feature extraction config

    high_gamma_bands = [(70, 90), (90, 110), (110, 130), (130, 150)]
    window_ms = 50
    stride_ms = 10
    spatial_filter = "CAR"  # common average reference
    z_score_baseline = True

Cross-referencing with [[Real-Time Speech Synthesis Engine]] for end-to-end latency budget. Current bottleneck is the LM rescoring step at ~120 ms per beam update. #decoding #real-time
`,
	},
	{
		Title:   "Real-Time Speech Synthesis Engine",
		Project: 12,
		Tags:    []string{"synthesis", "real-time", "tts"},
		Body: `## Real-Time Speech Synthesis Engine

The synthesis engine converts decoded phoneme sequences into audible speech output. We use a modified VITS (Variational Inference with adversarial learning for end-to-end Text-to-Speech) model fine-tuned for low-latency streaming output.

### Design Requirements

- **Latency**: Total decode-to-audio must be < 500 ms for conversational viability
- **Voice personalization**: Synthesized voice should approximate the participant's pre-injury voice when recordings are available
- **Streaming**: Audio generated incrementally as phonemes arrive, not waiting for full utterance

### Architecture

The engine receives phoneme posterior distributions from [[Phoneme Classification Architecture]] and operates in two modes:

1. **Streaming mode** -- Generates audio chunks every 100 ms using a causal WaveGlow vocoder. Trades quality for latency.
2. **Utterance mode** -- Buffers until end-of-utterance detected (silence > 600 ms), then runs full VITS pipeline. Higher quality.

### Voice Cloning

For participant P07 (see [[Participant P07 Session Notes]]), we collected 4.2 hours of pre-injury speech from family recordings. The speaker embedding was extracted using a d-vector model and injected into the VITS decoder conditioning.

Perceptual similarity scores (MOS-like, rated by family):

| Condition | Score (1-5) |
|-----------|-------------|
| Pre-injury recording | 4.8 |
| Cloned voice (utterance mode) | 3.6 |
| Cloned voice (streaming mode) | 3.1 |
| Default generic voice | 2.9 |

### Integration Points

The synthesis engine exposes a gRPC interface consumed by the [[Speech Prosthesis UI Layer]]. Audio is streamed as 16-bit PCM at 22.05 kHz via chunked responses. The [[Sensory Feedback Integration API]] is being explored for haptic confirmation of decoded words. #tts #latency
`,
	},
	{
		Title:   "Language Model Integration for Speech Decoding",
		Project: 12,
		Tags:    []string{"language-model", "decoding", "nlp"},
		Body: `## Language Model Integration for Speech Decoding

Integrating a language model (LM) into the speech decoding pipeline dramatically improves word-level accuracy by resolving phoneme ambiguities. This note documents our LM integration strategy and current results.

### Approach

We use constrained beam search where the phoneme classifier provides per-frame posterior probabilities, and a character-level language model scores candidate sequences. The beam search explores the top-K phoneme hypotheses at each timestep and prunes based on combined acoustic + LM score.

### LM Options Evaluated

| Model | Parameters | Perplexity | Latency (per token) | Chosen |
|-------|-----------|------------|---------------------|--------|
| 5-gram KenLM | N/A | 82.3 | 0.1 ms | Baseline |
| LSTM-LM | 24M | 54.1 | 2.3 ms | No |
| DistilGPT-2 | 82M | 41.7 | 8.1 ms | Yes |
| GPT-2 Medium | 345M | 34.2 | 31 ms | Too slow |

DistilGPT-2 provides the best accuracy-latency tradeoff. We run it quantized to INT8 on the edge GPU.

### Vocabulary Constraints

The LM operates over a restricted vocabulary tuned per participant. For locked-in patients, we prioritize:

- Basic needs (pain, thirst, temperature)
- Yes/no/maybe responses
- Names of caregivers and family
- Medical terminology relevant to their care

This restriction reduces perplexity to ~18 and boosts effective word accuracy from 68% to 89%.

### Integration with Decoder

    beam_width = 20
    lm_weight = 0.35
    acoustic_weight = 0.65
    max_candidates_per_frame = 5

The beam search runs asynchronously from the [[Phoneme Classification Architecture]] feature extraction. Results feed into [[Real-Time Speech Synthesis Engine]]. We are evaluating the [[Transfer Learning Across Participants]] techniques from Neural Signal AI to bootstrap LM personalization faster. #nlp #beam-search
`,
	},
	{
		Title:   "Speech Prosthesis UI Layer",
		Project: 12,
		Tags:    []string{"ui", "accessibility", "frontend"},
		Body: `## Speech Prosthesis UI Layer

The UI layer provides visual feedback and control interfaces for both participants and caregivers during speech prosthesis sessions.

### User Roles

1. **Participant** -- Locked-in or severely motor-impaired. Interacts via neural decoding only. Needs clear visual feedback of decoded output.
2. **Caregiver** -- Manages session start/stop, vocabulary customization, error correction. Touch/mouse interface.
3. **Clinician** -- Reviews session logs, adjusts decoder parameters, monitors signal quality. Full dashboard access.

### Participant Display

The participant-facing display shows:

- **Current decoded text** in large font (minimum 48pt) on high-contrast background
- **Confidence indicator** -- color bar showing decoder certainty (green > 80%, yellow 50-80%, red < 50%)
- **Word prediction bar** -- top 3 next-word candidates from the [[Language Model Integration for Speech Decoding]]
- **Utterance history** -- scrolling log of completed sentences

### Caregiver Panel

- Quick-select phrase boards for common needs
- Manual text override (type message on behalf of participant)
- Vocabulary editor for adding custom words/phrases
- Session timer and break reminders

### Technical Stack

The UI is a React application communicating via WebSocket with the backend decoding server. We reuse components from the [[SDK Architecture Overview]] for session management widgets. State management follows the pattern established in the BCI Cloud Platform's [[Real-Time Telemetry Dashboard]].

### Accessibility Considerations

- All UI elements must meet WCAG AAA contrast ratios (minimum 7:1)
- No reliance on color alone for status communication
- Audio confirmation of decoded words (via [[Real-Time Speech Synthesis Engine]])
- Caregiver panel supports switch scanning for motor-impaired operators

Screen layouts are documented in the shared Figma project. #ui #accessibility
`,
	},
	{
		Title:   "Attempted Speech Detection Model",
		Project: 12,
		Tags:    []string{"detection", "vad", "neural-signal"},
		Body: `## Attempted Speech Detection Model

Before phoneme classification can begin, we must detect when the participant is attempting to speak versus resting. This is the neural analog of voice activity detection (VAD) in traditional speech systems.

### Problem Statement

Locked-in patients cannot produce overt speech. We detect *attempted* speech from motor cortex activation patterns associated with articulatory planning. The detector must:

- Achieve onset detection within 150 ms of speech attempt
- Maintain false positive rate < 2% per minute during rest
- Handle variable baseline neural activity across sessions

### Model Architecture

Binary classifier (speech-attempt vs. rest) using features from ventral premotor cortex and primary motor cortex channels. Architecture:

    Input: 256-ch high-gamma features, 200ms window
    Layer 1: 1D Conv (128 filters, kernel=5) + BatchNorm + ReLU
    Layer 2: 1D Conv (64 filters, kernel=3) + BatchNorm + ReLU
    Layer 3: Global Average Pooling
    Layer 4: Dense(32) + ReLU + Dropout(0.3)
    Output: Sigmoid (speech probability)

### Calibration

The model requires per-session calibration using the [[Decoder Calibration Protocol]] from Cortical Decoder. Calibration takes approximately 5 minutes: 30 cued speech attempts interleaved with 30 rest periods of 3-5 seconds each.

### Current Performance

| Metric | Participant P07 | Participant P12 |
|--------|----------------|----------------|
| Sensitivity | 94.7% | 91.3% |
| Specificity | 98.2% | 97.1% |
| Onset latency (median) | 112 ms | 138 ms |
| Offset latency (median) | 89 ms | 104 ms |

The detector gates the downstream [[Phoneme Classification Architecture]] pipeline. When speech probability exceeds 0.7 for > 80 ms, the phoneme classifier begins processing. This reduces computational load and eliminates hallucinated phonemes during rest. #vad #detection
`,
	},
	{
		Title:   "Electrode Coverage for Speech Areas",
		Project: 12,
		Tags:    []string{"electrodes", "neurosurgery", "coverage"},
		Body: `## Electrode Coverage for Speech Areas

Optimal electrode placement is critical for high-accuracy speech decoding. This note documents our target cortical regions and coverage requirements for the speech prosthesis array.

### Target Regions

Speech production involves a distributed cortical network. Our coverage priorities, ranked by decoding contribution:

1. **Ventral premotor cortex (vPMC)** -- Articulatory planning. Highest phoneme discriminability.
2. **Primary motor cortex (M1), face/tongue area** -- Motor execution signals for articulation.
3. **Supplementary motor area (SMA)** -- Speech initiation and sequencing.
4. **Superior temporal gyrus (STG)** -- Auditory feedback (relevant for attempted speech imagery).
5. **Broca's area (IFG)** -- Language formulation (lower priority for motor decoding).

### Array Configuration

We use high-density ECoG grids manufactured per the [[Cleanroom Fabrication Process]] specifications:

| Region | Array Type | Channels | Spacing | Coverage Area |
|--------|-----------|----------|---------|---------------|
| vPMC + M1 face | HD-ECoG grid | 128 | 1.5 mm | 18 x 12 mm |
| SMA | Strip electrode | 32 | 3 mm | 96 mm linear |
| STG | HD-ECoG grid | 64 | 2 mm | 14 x 10 mm |
| Broca's area | Strip electrode | 32 | 3 mm | 96 mm linear |

### Implantation Considerations

Electrode placement follows neurosurgical planning based on pre-operative fMRI and DTI. The surgical team uses neuronavigation to align grids with functional landmarks identified during the [[Phase I Trial Design]] screening protocol.

For wireless data transmission from implanted arrays, we rely on the [[RF Link Budget Analysis]] from the Implant Telemetry team to verify sufficient bandwidth for 256-channel streaming at 30 kHz.

### Signal Quality Metrics

Post-implant signal quality is assessed within 48 hours. Channels with impedance > 50 kOhm or SNR < 3 dB are flagged. Historically, 8-12% of channels are excluded per participant. See [[Participant P07 Session Notes]] for a representative example. #electrodes #implant #neurosurgery
`,
	},
	{
		Title:   "Phoneme Confusion Analysis",
		Project: 12,
		Tags:    []string{"analysis", "phoneme", "accuracy"},
		Body: `## Phoneme Confusion Analysis

Systematic analysis of phoneme classification errors reveals patterns that inform both model improvements and language model weighting strategies.

### Methodology

Confusion matrices were generated from 12 sessions across 3 participants using the [[Phoneme Classification Architecture]] TCN model. Each session contained 200-400 cued phoneme attempts. Ground truth was established from the visual cue presented (not acoustic output, since participants cannot produce overt speech).

### Most Confused Phoneme Pairs

| Phoneme A | Phoneme B | Confusion Rate | Articulatory Similarity |
|-----------|-----------|---------------|------------------------|
| /b/ | /p/ | 23.4% | Bilabial stop, differ only in voicing |
| /d/ | /t/ | 19.8% | Alveolar stop, differ only in voicing |
| /m/ | /n/ | 17.1% | Nasal, differ in place (bilabial vs alveolar) |
| /s/ | /z/ | 15.6% | Fricative, differ only in voicing |
| /f/ | /v/ | 14.2% | Labiodental fricative, differ only in voicing |

### Key Findings

**Voicing distinctions are hardest to decode.** Voiced/unvoiced pairs share nearly identical articulatory gestures and differ primarily in laryngeal activity, which is poorly represented in cortical motor signals recorded from the surface.

**Manner of articulation is well preserved.** Stops, fricatives, nasals, and vowels form clearly separable clusters. Cross-manner confusion is rare (< 3%).

**Vowels are easiest.** The 11 monophthong vowels achieve 87% top-1 accuracy versus 68% for consonants. Vowels involve large, distinct tongue body movements well represented in M1.

### Mitigation Strategies

1. Merge voiced/unvoiced pairs into single phoneme classes and let the [[Language Model Integration for Speech Decoding]] resolve voicing from context
2. Add high-gamma features from STG channels (auditory imagery may encode voicing)
3. Explore the [[Spike Sorting Algorithm Comparison]] to determine if single-unit resolution improves voicing discrimination

This analysis feeds directly into the confusion-aware loss function used during model training. #phoneme #confusion #analysis
`,
	},
	{
		Title:   "Participant Enrollment and Consent Protocol",
		Project: 12,
		Tags:    []string{"clinical", "consent", "protocol"},
		Body: `## Participant Enrollment and Consent Protocol

This document outlines the enrollment and informed consent process for participants in the speech prosthesis study. All procedures comply with IRB approval #2024-BCI-SP-003.

### Eligibility Criteria

**Inclusion:**
- Age 18-75
- Diagnosis of locked-in syndrome (LIS) or severe dysarthria
- Preserved cognitive function (assessed via eye-tracking communication system)
- Stable neurological condition for >= 6 months
- Willing caregiver available for study participation

**Exclusion:**
- Active infection or immunocompromised state
- Contraindication for craniotomy
- Prior brain surgery in target implant region
- Inability to provide informed consent (even via assistive means)

### Consent Process

Given that participants cannot sign documents, we follow a witnessed multimedia consent process:

1. Study information presented via accessible video (eye-tracking controlled)
2. Q&A session with PI using participant's existing communication system
3. Comprehension assessment -- 10 questions about study risks and procedures
4. Consent recorded on video with two independent witnesses
5. Legally authorized representative also signs written consent

This process aligns with the broader [[Participant Recruitment Strategy]] established by the Clinical Trials Program. Regulatory requirements follow the [[FDA 510k Submission Timeline]] for investigational device exemption.

### Enrollment Targets

| Phase | Participants | Timeline | Status |
|-------|-------------|----------|--------|
| Pilot (safety) | 3 | Q1-Q2 2025 | Complete |
| Feasibility | 8 | Q3 2025 - Q1 2026 | Enrolling |
| Pivotal | 20 | Q2 2026 - Q4 2027 | Planned |

### Data Collection

All session data is stored per [[Neural Data Anonymization]] protocols. Participant identifiers are replaced with study codes (P01, P02, etc.) at the point of collection. #clinical #enrollment
`,
	},
	{
		Title:   "Decoder Latency Budget Analysis",
		Project: 12,
		Tags:    []string{"latency", "performance", "real-time"},
		Body: `## Decoder Latency Budget Analysis

End-to-end latency from neural activity to audible speech must remain below 500 ms for the system to feel conversational. This document breaks down the latency budget across pipeline stages.

### Latency Budget

| Stage | Target (ms) | Measured (ms) | Status |
|-------|------------|--------------|--------|
| Signal acquisition + filtering | 20 | 18 | OK |
| Feature extraction (high-gamma) | 50 | 42 | OK |
| Speech attempt detection | 30 | 28 | OK |
| Phoneme classification (TCN) | 15 | 12 | OK |
| Beam search + LM rescoring | 100 | 145 | OVER |
| Phoneme-to-audio synthesis | 80 | 73 | OK |
| Audio buffer + DAC | 20 | 20 | OK |
| **Total** | **315** | **338** | Marginal |

### Bottleneck: LM Rescoring

The [[Language Model Integration for Speech Decoding]] beam search consistently exceeds its 100 ms budget. Root causes:

1. DistilGPT-2 forward pass takes 8 ms per candidate, and beam width of 20 means up to 160 ms worst case
2. KV-cache management adds overhead during long utterances
3. INT8 quantization on the Jetson AGX provides only 1.5 TOPS effective throughput

### Proposed Optimizations

- Reduce beam width from 20 to 10 (estimated -40 ms, minimal accuracy loss per [[Phoneme Confusion Analysis]])
- Implement speculative decoding with the KenLM n-gram as draft model
- Migrate to TensorRT for GPU inference (estimated 2x speedup)
- Pipeline the LM scoring: begin rescoring as phonemes arrive rather than waiting for full window

### Hardware Considerations

Current compute platform is NVIDIA Jetson AGX Orin. The [[Implant Firmware OTA Update Protocol]] team is evaluating on-implant preprocessing that could shift 20-30 ms of feature extraction latency off the external processor. This would free budget for the LM stage. #latency #optimization #real-time
`,
	},
	{
		Title:   "Multi-Participant Generalization Study",
		Project: 12,
		Tags:    []string{"generalization", "transfer-learning", "study"},
		Body: `## Multi-Participant Generalization Study

A key challenge for clinical deployment is reducing the per-participant calibration burden. This study evaluates cross-participant transfer learning for the speech prosthesis decoder.

### Background

Currently, the [[Phoneme Classification Architecture]] requires 8-10 hours of participant-specific training data collected over 2-3 weeks. This is burdensome for locked-in patients. We hypothesize that a pretrained base model can be fine-tuned with significantly less participant-specific data.

### Study Design

**Phase 1: Base model training**
- Pool data from participants P01-P07 (combined 62 hours of decoded speech attempts)
- Train a shared TCN encoder with participant-specific linear output heads
- Evaluate zero-shot transfer to held-out participant P08

**Phase 2: Few-shot fine-tuning**
- Fine-tune the shared encoder on P08 data in increments (15 min, 30 min, 1 hr, 2 hr, 4 hr)
- Compare accuracy against participant-specific model trained from scratch

### Preliminary Results

    P08 phoneme accuracy vs. calibration data:
    
    Zero-shot (no P08 data):     41.2%
    15 min fine-tune:            58.7%
    30 min fine-tune:            65.3%
    1 hr fine-tune:              71.8%
    2 hr fine-tune:              76.1%
    4 hr fine-tune:              78.4%
    Full training (10 hr):       79.2%

The 2-hour fine-tuning achieves 96% of the full-training accuracy, representing a 5x reduction in calibration time.

### Alignment with Neural Signal AI

We are collaborating closely with the Neural Signal AI team, whose [[Transfer Learning Across Participants]] framework provides the domain adaptation layers used in our shared encoder. Their [[Training Data Pipeline]] handles the cross-participant data normalization (electrode remapping, amplitude scaling).

### Next Steps

- Extend to participants with different electrode configurations (currently all use the same 256-ch layout per [[Electrode Coverage for Speech Areas]])
- Evaluate online adaptation where the model continues learning during regular use
- Test with the [[Decoder Calibration Protocol]] shortened to 30-minute sessions #transfer-learning #generalization
`,
	},
	{
		Title:   "Speech Prosthesis Integration Testing Plan",
		Project: 12,
		Tags:    []string{"testing", "integration", "qa"},
		Body: `## Speech Prosthesis Integration Testing Plan

This document defines the integration testing strategy for the speech prosthesis system, covering component interfaces, end-to-end validation, and clinical acceptance criteria.

### Test Architecture

The system comprises five major components that must be tested both individually and in combination:

1. Signal acquisition (hardware + firmware)
2. Feature extraction + speech detection ([[Attempted Speech Detection Model]])
3. Phoneme decoder ([[Phoneme Classification Architecture]])
4. Language model ([[Language Model Integration for Speech Decoding]])
5. Speech synthesis ([[Real-Time Speech Synthesis Engine]])

### Integration Test Scenarios

**Scenario 1: Decode-to-Text**
- Input: Pre-recorded ECoG data from participant sessions
- Expected: Phoneme sequence matches ground truth within confusion tolerance
- Pass criteria: Word error rate < 25% on 50-word test set

**Scenario 2: End-to-End Latency**
- Input: Simulated neural trigger at known timestamp
- Expected: Audio output begins within 500 ms
- Measurement: Hardware timestamp comparison per [[Decoder Latency Budget Analysis]]

**Scenario 3: Session Lifecycle**
- Start session, run calibration, decode 5 minutes of attempted speech, end session
- Verify all data logged correctly per [[Session Recording and Replay]] format
- Verify [[HIPAA Compliance Checklist]] fields are properly anonymized

**Scenario 4: Graceful Degradation**
- Simulate channel dropout (25%, 50% of electrodes)
- System should reduce accuracy gracefully, not crash
- Alert caregiver via [[Speech Prosthesis UI Layer]] when quality drops below threshold

### Test Data Management

    test_sessions/
        P07_session_042/
            raw_ecog.npy        # 256 channels, 30 kHz
            events.json         # cue timestamps
            ground_truth.txt    # expected phoneme sequence
            metadata.yaml       # session parameters

### Schedule

Integration tests run nightly on the CI server. Full end-to-end tests with hardware-in-the-loop run weekly. Results are published to the [[Real-Time Telemetry Dashboard]]. #testing #integration
`,
	},
	{
		Title:   "Locked-In Patient Communication Benchmarks",
		Project: 12,
		Tags:    []string{"benchmarks", "clinical", "communication"},
		Body: `## Locked-In Patient Communication Benchmarks

Establishing meaningful communication benchmarks requires metrics that go beyond traditional speech recognition accuracy. This note defines the evaluation framework for our speech prosthesis in the context of locked-in patient communication.

### Communication Rate Metrics

| Metric | Current | Target | State-of-Art (literature) |
|--------|---------|--------|--------------------------|
| Characters per minute (CPM) | 32 | 60 | 62 (Willett et al., 2023) |
| Words per minute (WPM) | 7.1 | 15 | 15.2 (Metzger et al., 2023) |
| Correct words per minute | 5.8 | 12 | 12.0 (Metzger et al., 2023) |
| Conversation turns per hour | 18 | 40 | N/A |

### Quality of Life Metrics

Communication rate alone does not capture clinical impact. We also track:

- **Communication Independence Scale (CIS)** -- custom 7-point scale measuring how often the participant can communicate without caregiver interpretation
- **Participant satisfaction survey** -- administered monthly via eye-tracking interface
- **Caregiver burden assessment** -- time spent facilitating communication pre vs. post implant
- **Social participation index** -- frequency and duration of conversations with family/friends

### Benchmark Protocol

Each evaluation session follows a structured protocol:

1. **Free conversation** (10 min) -- participant communicates whatever they wish
2. **Copy task** (5 min) -- participant attempts to reproduce displayed sentences
3. **Question answering** (5 min) -- caregiver asks predetermined questions
4. **Emergency phrase test** -- participant must produce 5 critical phrases (pain, help, stop, yes, no)

Sessions are recorded per [[Session Recording and Replay]] specifications. Decoded text is scored by two independent raters. Inter-rater reliability must exceed Cohen's kappa of 0.85.

### Comparison with Existing Systems

Our system is compared against the participant's existing communication method (typically eye-tracking with on-screen keyboard). The [[Phase I Trial Design]] primary endpoint requires our system to match or exceed existing method CPM within 30 days of decoder stabilization. #benchmarks #outcomes
`,
	},
	{
		Title:   "Articulatory Feature Representation",
		Project: 12,
		Tags:    []string{"features", "articulatory", "phonetics"},
		Body: `## Articulatory Feature Representation

Rather than classifying raw phonemes directly, we explore an intermediate representation based on articulatory features -- the physical gestures involved in speech production. This approach better matches the motor cortex signals we record.

### Articulatory Feature Set

We decompose each phoneme into 6 articulatory dimensions:

| Feature | Values | Example |
|---------|--------|---------|
| Manner | stop, fricative, nasal, approximant, vowel | /b/ = stop |
| Place | bilabial, labiodental, alveolar, postalveolar, velar, glottal | /b/ = bilabial |
| Voicing | voiced, unvoiced | /b/ = voiced |
| Lip rounding | rounded, unrounded | /u/ = rounded |
| Tongue height | high, mid, low | /i/ = high |
| Tongue backness | front, central, back | /i/ = front |

### Architecture

Instead of a single 39-class phoneme classifier, we train 6 parallel binary/multi-class classifiers, one per articulatory dimension. The outputs are combined to reconstruct the most likely phoneme.

    Input: high-gamma features (256 ch x 50 ms)
        |
        +-> Manner classifier (5-class) ----+
        +-> Place classifier (6-class) -----+
        +-> Voicing classifier (binary) ----+--> Phoneme reconstruction
        +-> Lip rounding classifier (binary)+
        +-> Tongue height (3-class) --------+
        +-> Tongue backness (3-class) ------+

### Advantages

This decomposition addresses the voicing confusion problem identified in [[Phoneme Confusion Analysis]]. Even if the voicing classifier fails, the manner + place classifiers can narrow candidates to 2 phonemes, which the [[Language Model Integration for Speech Decoding]] can disambiguate with high confidence.

### Neural Correlates

Preliminary electrode-level analysis shows that articulatory features map to distinct cortical subregions, consistent with somatotopic organization of the face/tongue area in M1. This spatial separation was confirmed using the electrode mapping from [[Electrode Coverage for Speech Areas]].

Feature-level accuracy ranges from 82% (voicing) to 96% (manner), compared to 74% for direct phoneme classification. #articulatory #features #motor-cortex
`,
	},
	{
		Title:   "Speech Prosthesis Regulatory Pathway",
		Project: 12,
		Tags:    []string{"regulatory", "fda", "clinical"},
		Body: `## Speech Prosthesis Regulatory Pathway

The speech prosthesis is classified as a Class III active implantable medical device. This note outlines our regulatory strategy for US and EU markets.

### US FDA Pathway

We are pursuing a De Novo classification pathway, as there is no existing predicate device for cortically-driven speech prostheses. Key milestones:

1. **Pre-submission meeting** -- Completed 2024-09. FDA agreed to De Novo pathway with clinical evidence from a pivotal trial.
2. **IDE application** -- Submitted 2025-01 for the pivotal trial. References the [[FDA 510k Submission Timeline]] established for the broader BCI platform.
3. **Breakthrough Device designation** -- Granted 2024-07. Enables priority review and interactive communication with FDA.
4. **Pivotal trial** -- 20 participants, 12-month follow-up. Primary endpoint: communication rate improvement >= 50% vs. baseline assistive device.
5. **De Novo submission** -- Target Q1 2028.

### EU MDR Pathway

Under EU MDR, the device falls under Class III (Rule 8 -- active therapeutic devices). We are coordinating with the Regulatory Affairs team on [[EU MDR Classification]] documentation. A notified body has been pre-selected; technical file preparation begins Q2 2026.

### Software Classification

The decoding software (including the [[Phoneme Classification Architecture]] and [[Language Model Integration for Speech Decoding]]) is classified as Software as a Medical Device (SaMD). Risk classification per IMDRF:

| Factor | Classification |
|--------|---------------|
| State of healthcare situation | Serious (communication for critical care) |
| Significance of information | Treat or diagnose |
| **SaMD Category** | **III (high)** |

### Quality Management

- Design controls per ISO 13485 and IEC 62304
- Software lifecycle: IEC 62304 Class C (highest risk)
- Cybersecurity: Pre-market guidance compliance, including [[HIPAA Compliance Checklist]] alignment
- Post-market surveillance plan includes real-world performance monitoring via [[Real-Time Telemetry Dashboard]]

#regulatory #fda #compliance
`,
	},
	{
		Title:   "Game Controller Abstraction Layer",
		Project: 13,
		Tags:    []string{"architecture", "controller", "abstraction"},
		Body: `## Game Controller Abstraction Layer

The BCI Gaming Platform must translate neural intent signals into standard game controller inputs. The abstraction layer maps variable BCI output to a unified controller interface that game developers can target without BCI-specific knowledge.

### Architecture

    Neural Input -> Intent Classifier -> Controller Abstraction -> Game Engine

The abstraction layer exposes a virtual controller with the following capabilities:

| Input Type | BCI Mapping | Equivalent Gamepad |
|-----------|-------------|-------------------|
| Discrete actions (4-8 classes) | Motor imagery classification | D-pad / face buttons |
| Continuous 2D cursor | Decoded hand kinematics | Analog stick |
| Binary trigger | Sustained attention / jaw clench | Shoulder trigger |
| Menu navigation | SSVEP selection | Start/Select |

### Interface Definition

    type BCIController interface {
        // Discrete intent with confidence
        GetDiscreteAction() (action Action, confidence float64)
        
        // Continuous 2D position normalized to [-1, 1]
        GetCursorPosition() (x, y float64)
        
        // Binary sustained activation
        GetTriggerState() (active bool, duration time.Duration)
        
        // SSVEP-based menu selection
        GetMenuSelection(options int) (selected int, err error)
    }

The interface follows patterns from the [[SDK Architecture Overview]] to ensure consistency with other BCI applications in the lab ecosystem. Game developers interact with this interface via the [[REST API Specification]] for session management and WebSocket for real-time input streaming.

### Latency Requirements

Gaming demands tighter latency than most BCI applications. Our target is < 100 ms from neural activity to game engine input event. This is informed by the latency analysis in the [[Decoder Latency Budget Analysis]] from the Speech Prosthesis project, adapted for motor imagery decoding which has a simpler classification pipeline.

### Supported Game Engines

- Unity (C# plugin) -- primary target
- Godot (GDScript binding) -- secondary
- Web games (JavaScript SDK via WebSocket) #controller #abstraction #architecture
`,
	},
	{
		Title:   "Motor Imagery Intent Classification",
		Project: 13,
		Tags:    []string{"classification", "motor-imagery", "ml"},
		Body: `## Motor Imagery Intent Classification

The core BCI input for the gaming platform uses motor imagery (MI) -- imagined movements of different body parts -- classified from EEG signals. This note documents the classification pipeline and performance benchmarks.

### Signal Acquisition

We use the lab's non-invasive EEG system (see [[Dry Electrode Contact Optimization]]) with 32 channels focused on the sensorimotor cortex (C3, Cz, C4 and surrounding positions). Sampling rate is 250 Hz, bandpass filtered 4-40 Hz.

### Classification Pipeline

1. **Preprocessing** -- Artifact rejection (eye blinks, muscle), spatial filtering via Common Spatial Patterns (CSP)
2. **Feature extraction** -- Band power features in mu (8-13 Hz) and beta (13-30 Hz) bands, computed in 500 ms windows with 100 ms stride
3. **Classification** -- Shrinkage LDA for 4-class MI (left hand, right hand, feet, tongue)

### Performance Benchmarks

Tested on 24 healthy participants in a cued motor imagery paradigm:

| Metric | 2-class | 4-class | 8-class |
|--------|---------|---------|---------|
| Mean accuracy | 84.2% | 71.8% | 52.3% |
| Median ITR (bits/min) | 18.4 | 22.1 | 19.7 |
| Latency (decision time) | 420 ms | 620 ms | 890 ms |
| BCI illiteracy rate | 8% | 15% | 31% |

The 4-class configuration offers the best information transfer rate (ITR) and maps naturally to directional game input.

### Adaptive Classification

Player skill improves over time (neuroplasticity), so the classifier must adapt:

    // Online adaptation with exponential forgetting
    func (c *Classifier) AdaptOnline(features []float64, label int) {
        c.mu.Lock()
        defer c.mu.Unlock()
        c.updateCovarianceMatrices(features, label, c.forgettingFactor)
        c.recomputeCSPFilters()
    }

This approach mirrors the adaptive strategies used in the [[Locomotion Decoder Design]] for the Spinal Cord Interface project. Training data is managed via the [[Training Data Pipeline]]. #motor-imagery #classification #eeg
`,
	},
	{
		Title:   "Low-Latency Networking for BCI Multiplayer",
		Project: 13,
		Tags:    []string{"networking", "multiplayer", "latency"},
		Body: `## Low-Latency Networking for BCI Multiplayer

Multiplayer BCI gaming introduces unique networking challenges because input latency is already high (400-600 ms from intent to classification) compared to traditional gaming (< 10 ms button press). We must minimize additional network latency.

### Architecture

    Player A BCI -> Local Classifier -> Game Client A
                                            |
                                      WebSocket (< 20 ms RTT)
                                            |
                                      Game Server
                                            |
                                      WebSocket (< 20 ms RTT)
                                            |
    Player B BCI -> Local Classifier -> Game Client B

### Design Decisions

**Client-side prediction**: Each client runs the game simulation locally and applies BCI inputs immediately. Server reconciliation corrects discrepancies. This hides network latency from the player but requires deterministic game logic.

**Input compression**: BCI inputs are compact (action enum + confidence float + timestamp = 16 bytes). No need for delta compression.

**Tick rate**: 20 Hz server tick rate (50 ms). Higher rates are unnecessary given BCI input latency. This is much lower than traditional competitive games (64-128 Hz) and reduces server cost.

### Synchronization Protocol

    message BCIInput {
        uint32 player_id = 1;
        uint64 timestamp_us = 2;
        Action action = 3;
        float confidence = 4;
        float cursor_x = 5;  // optional, for continuous input
        float cursor_y = 6;
    }

The server buffers inputs for up to 200 ms to compensate for clock skew and variable BCI classification latency across players. This is significantly more generous than traditional netcode because BCI timing is inherently variable.

### Infrastructure

The multiplayer backend runs on the [[Multi-Site Data Sync Architecture]] infrastructure from BCI Cloud Platform. Session telemetry is streamed to the [[Real-Time Telemetry Dashboard]] for monitoring concurrent player counts and latency distributions.

### Fairness Considerations

Players using different BCI hardware (invasive vs. non-invasive) have vastly different input latencies and accuracies. We implement handicap systems based on device class, not player skill, to ensure competitive fairness. See [[Game Balance and Difficulty Adaptation]]. #multiplayer #networking
`,
	},
	{
		Title:   "SSVEP Menu Navigation System",
		Project: 13,
		Tags:    []string{"ssvep", "ui", "navigation"},
		Body: `## SSVEP Menu Navigation System

Steady-State Visual Evoked Potential (SSVEP) provides a reliable, high-accuracy BCI input method for menu navigation in the gaming platform. Unlike motor imagery, SSVEP requires minimal training and achieves > 90% accuracy in most users.

### Principle

SSVEP exploits the brain's response to flickering visual stimuli. Each menu option flickers at a unique frequency (e.g., 7 Hz, 9 Hz, 11 Hz, 13 Hz). The user gazes at their desired option, and the corresponding frequency appears in their occipital EEG.

### Frequency Selection

Frequencies must be carefully chosen to avoid harmonics and maintain visual comfort:

| Menu Position | Frequency (Hz) | Duty Cycle | Harmonic Conflicts |
|--------------|----------------|------------|-------------------|
| Top | 7.0 | 50% | 14 Hz (2nd) -- outside range |
| Right | 8.5 | 50% | 17 Hz (2nd) -- outside range |
| Bottom | 10.0 | 50% | 20 Hz (2nd) -- marginal |
| Left | 12.0 | 50% | 24 Hz (2nd) -- outside range |

We optimized these frequencies based on findings from the [[SSVEP Paradigm Optimization]] research by the Non-Invasive EEG Headset team.

### Detection Algorithm

Canonical Correlation Analysis (CCA) compares EEG from O1, Oz, O2 channels against reference sinusoids at each target frequency:

    func (d *SSVEPDetector) Classify(eeg [][]float64) (int, float64) {
        maxCorr := 0.0
        selected := -1
        for i, freq := range d.frequencies {
            ref := d.generateReference(freq, d.harmonics)
            corr := canonicalCorrelation(eeg, ref)
            if corr > maxCorr {
                maxCorr = corr
                selected = i
            }
        }
        return selected, maxCorr
    }

### Integration with Game UI

SSVEP is used exclusively for menu navigation, not in-game control (flickering would be distracting during gameplay). The [[Game Controller Abstraction Layer]] switches between MI mode (gameplay) and SSVEP mode (menus) based on game state. Detection window is 2 seconds, yielding selection rates of ~30 selections/minute. The signal quality depends on the headset; see [[Consumer EEG Signal Quality]] for device comparison. #ssvep #menu #navigation
`,
	},
	{
		Title:   "Game Balance and Difficulty Adaptation",
		Project: 13,
		Tags:    []string{"game-design", "adaptation", "accessibility"},
		Body: `## Game Balance and Difficulty Adaptation

BCI input is inherently noisier and slower than traditional game controllers. The gaming platform must adapt difficulty and game mechanics to maintain engagement without frustration.

### BCI Performance Profile

Each player has a dynamically updated performance profile:

    type PlayerProfile struct {
        ClassificationAccuracy float64    // rolling 100-trial average
        MeanReactionTime       time.Duration
        InputRate              float64    // actions per minute
        FatigueIndex           float64    // decreasing accuracy trend
        SessionDuration        time.Duration
        DeviceClass            string     // "invasive", "eeg-wet", "eeg-dry"
    }

### Adaptive Difficulty Algorithm

The difficulty controller adjusts game parameters every 60 seconds based on player performance:

| Performance Range | Difficulty Adjustment |
|------------------|----------------------|
| Accuracy > 85%, low fatigue | Increase speed, add complexity |
| Accuracy 65-85% | Maintain current difficulty |
| Accuracy 45-65% | Reduce speed, simplify choices |
| Accuracy < 45% | Switch to binary-choice mode |

### Game Design Guidelines

For developers building games on the platform:

1. **No twitch reactions** -- Minimum response window of 800 ms for any time-critical action
2. **Undo/confirm patterns** -- All irreversible actions require confirmation (decode errors are common)
3. **Turn-based preferred** -- Turn-based games naturally accommodate BCI latency
4. **Reduced action space** -- Design around 4-6 distinct actions maximum per game state
5. **Visual feedback** -- Show decoded intent before executing action (200 ms preview window)

### Fatigue Management

BCI use causes mental fatigue. The platform enforces rest breaks:

- Mandatory 30-second rest every 10 minutes
- Suggested longer break at 30 minutes
- Session auto-ends at 60 minutes with option to extend

Fatigue detection uses accuracy trend analysis from the [[Motor Imagery Intent Classification]] pipeline. The approach to adaptive difficulty borrows from the [[Adaptive Therapy Engine]] in the Neurorehab project. #game-design #difficulty #accessibility
`,
	},
	{
		Title:   "BCI Gaming Platform Architecture Overview",
		Project: 13,
		Tags:    []string{"architecture", "platform", "overview"},
		Body: `## BCI Gaming Platform Architecture Overview

High-level architecture of the BCI Gaming Platform, covering system components, data flow, and deployment topology.

### System Components

    +-------------------+     +------------------+     +----------------+
    |  BCI Hardware     |     |  Local Client    |     |  Cloud Server  |
    |  (EEG Headset)    |---->|  Application     |---->|  (Multiplayer) |
    +-------------------+     +------------------+     +----------------+
                              |  - Signal proc   |     |  - Matchmaking |
                              |  - Classifier    |     |  - Game state  |
                              |  - Game engine   |     |  - Analytics   |
                              |  - Rendering     |     |  - Profiles    |
                              +------------------+     +----------------+

### Technology Stack

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Signal processing | Go (cgo-free) | Performance, cross-platform |
| Classifier | ONNX Runtime | Portable ML inference |
| Game engine | Godot 4.3 | Open source, accessible |
| Cloud backend | Go + PostgreSQL | Consistency with lab stack |
| Real-time comms | WebSocket + Protobuf | Low overhead |
| Analytics | ClickHouse | Time-series game events |

### Data Flow

1. EEG headset streams raw data over BLE to local client
2. Signal preprocessing filters and extracts features (see [[Motor Imagery Intent Classification]])
3. ONNX classifier produces intent labels
4. [[Game Controller Abstraction Layer]] maps intents to controller inputs
5. Game engine processes inputs, updates state
6. For multiplayer: state changes sent to cloud server via [[Low-Latency Networking for BCI Multiplayer]]

### Deployment Models

**Standalone**: Single player, all processing local. No internet required. Target for accessibility use cases.

**Connected**: Cloud multiplayer, leaderboards, profiles. Requires stable internet (< 50 ms RTT to server).

**Clinical**: Deployed in rehabilitation settings. Integrates with [[Neurorehab Therapy Suite]] for therapeutic gaming. Patient data handled per [[HIPAA Compliance Checklist]].

### SDK Integration

Game developers access BCI input through a plugin built on the [[SDK Architecture Overview]]. The plugin exposes the virtual controller interface and handles all signal processing transparently. API documentation follows the [[REST API Specification]] format. #architecture #platform
`,
	},
	{
		Title:   "Accessibility Testing Framework",
		Project: 13,
		Tags:    []string{"testing", "accessibility", "qa"},
		Body: `## Accessibility Testing Framework

The BCI Gaming Platform is inherently an accessibility product. Our testing framework ensures games are playable across a wide range of motor and cognitive abilities.

### Test Participant Profiles

We define 5 canonical accessibility profiles for testing:

| Profile | Motor Function | Cognitive | BCI Experience | Target Accuracy |
|---------|---------------|-----------|---------------|----------------|
| A: Power user | Full (healthy) | Full | > 50 hrs | > 80% |
| B: Experienced patient | Limited upper limb | Full | > 20 hrs | > 70% |
| C: New BCI user | Minimal | Full | < 5 hrs | > 50% |
| D: Cognitive impairment | Variable | Mild impairment | Variable | > 40% |
| E: Locked-in | None | Full | Variable | > 60% (invasive) |

### Automated Test Suite

Each game submitted to the platform must pass automated accessibility checks:

1. **Input timing audit** -- No action requires response in < 800 ms
2. **Choice complexity** -- No game state presents > 8 simultaneous action options
3. **Visual accessibility** -- All interactive elements > 48px touch target equivalent
4. **Color independence** -- Game state readable without color (pattern/shape differentiation)
5. **Pause support** -- Game can be paused at any point without penalty
6. **Session length** -- Games must support sessions of 5, 10, 20, 30 minutes

### BCI Simulation Testing

For automated CI/CD testing without real BCI hardware, we provide a simulated BCI input stream:

    type SimulatedBCI struct {
        accuracy    float64        // configurable error rate
        latency     time.Duration  // simulated classification delay
        fatigueRate float64        // accuracy decay per minute
    }

The simulator generates input sequences matching each accessibility profile, enabling regression testing without human participants. The simulation models are calibrated against real data from the [[Consumer EEG Signal Quality]] benchmarks.

### Compliance

All games must meet CVAA (21st Century Communications and Video Accessibility Act) requirements. Accessibility reports are generated per game and stored alongside platform analytics. #accessibility #testing #compliance
`,
	},
	{
		Title:   "BCI Esports Competition Framework",
		Project: 13,
		Tags:    []string{"esports", "competition", "community"},
		Body: `## BCI Esports Competition Framework

Competitive BCI gaming represents a unique esports category. This document outlines the framework for fair, engaging BCI competitions.

### Competition Structure

**Divisions by device class:**
- **Open Division** -- Any BCI device (invasive or non-invasive). Highest skill ceiling.
- **Consumer Division** -- Non-invasive EEG headsets only. Most accessible.
- **Clinical Division** -- Participants with motor impairments using prescribed BCI systems. Focuses on personal improvement.

### Fairness Rules

BCI competitions face unique fairness challenges:

1. **Hardware normalization** -- All participants in Consumer Division must use approved headset models. Signal quality baselines verified per [[Consumer EEG Signal Quality]] standards.
2. **Classifier restrictions** -- Only platform-provided classifiers allowed (no custom ML models that could give unfair advantage)
3. **Calibration time limits** -- Maximum 10-minute pre-match calibration session using the standard [[Decoder Calibration Protocol]]
4. **Fatigue monitoring** -- Matches limited to best-of-3 games, maximum 15 minutes each, with mandatory 5-minute breaks

### Game Requirements for Competitive Play

Not all platform games are suitable for competition. Competitive games must:

- Use the 4-class motor imagery input exclusively (standardized across all players)
- Have deterministic game logic (no RNG advantage)
- Support spectator mode with decoded-intent visualization
- Record full input streams for replay/dispute resolution via [[Session Recording and Replay]]

### Tournament Infrastructure

Tournaments use the multiplayer infrastructure described in [[Low-Latency Networking for BCI Multiplayer]]. Anti-cheat measures include:

- Real-time EEG signal monitoring to detect non-neural input injection
- Statistical analysis of input patterns to flag superhuman accuracy
- Hardware integrity checks via [[Dry Electrode Contact Optimization]] impedance monitoring

### Community Building

Monthly online tournaments with rankings. Annual in-person championship. Partnerships with disability sports organizations. All events streamed with accessibility-first production (captions, audio description). #esports #competition
`,
	},
	{
		Title:   "Audio-Haptic Feedback System for BCI Games",
		Project: 13,
		Tags:    []string{"feedback", "haptics", "audio"},
		Body: `## Audio-Haptic Feedback System for BCI Games

Effective feedback is crucial for BCI gaming because players cannot rely on traditional proprioceptive feedback from physical controller manipulation. This system provides multi-modal confirmation of decoded intents and game events.

### Feedback Modalities

| Modality | Purpose | Latency Target |
|----------|---------|---------------|
| Visual | Primary game feedback | < 16 ms (60 FPS) |
| Audio | Intent confirmation, game events | < 30 ms |
| Haptic | Intent confirmation (wearable) | < 50 ms |

### Audio Feedback Design

Each decoded intent triggers a distinct audio cue:

- **Action confirmed** -- Short rising tone (100 ms, 800 Hz -> 1200 Hz)
- **Action rejected** (low confidence) -- Soft click (50 ms, 400 Hz)
- **Selection hover** -- Subtle tick at 0.3x volume
- **Error/miss** -- Descending tone (150 ms, 600 Hz -> 300 Hz)

Audio spatialization matches the intent direction (left-hand MI triggers left-panned audio). This reinforces the motor imagery strategy and improves classification accuracy over time.

### Haptic Feedback

For players with residual sensation, a wrist-worn haptic actuator provides vibrotactile patterns:

    type HapticPattern struct {
        Frequency  float64       // vibration Hz
        Amplitude  float64       // 0.0 - 1.0
        Duration   time.Duration
        Waveform   WaveformType  // sine, square, pulse
    }

Haptic patterns are synchronized with audio cues. The design draws from the [[Micro-stimulation Parameter Space]] research in the Sensory Feedback Loop project, adapted for surface vibrotactile rather than cortical stimulation.

### Neural Feedback Loop

The most novel aspect: decoded intent is fed back to the player in real-time, creating a closed-loop BCI training effect. Players naturally learn to produce more separable neural patterns because they receive immediate feedback. This mirrors the closed-loop principles in [[Closed-Loop Grasp Controller]] but applied to entertainment rather than motor control.

### Integration

The feedback system is managed by the [[Game Controller Abstraction Layer]], which emits feedback events alongside input events. Game developers can customize feedback parameters per game context. #feedback #haptics #audio
`,
	},
	{
		Title:   "BCI Gaming Analytics Pipeline",
		Project: 13,
		Tags:    []string{"analytics", "data", "pipeline"},
		Body: `## BCI Gaming Analytics Pipeline

The analytics pipeline captures, processes, and visualizes player behavior and BCI performance data to inform game design, accessibility improvements, and platform health monitoring.

### Data Collection

Every gaming session generates the following event streams:

| Stream | Events/sec | Payload | Storage |
|--------|-----------|---------|---------|
| BCI input | 2-10 | Intent + confidence + timestamp | ClickHouse |
| Game state | 20 (tick rate) | Full game state snapshot | ClickHouse |
| Performance | 1 | Accuracy, latency, fatigue index | PostgreSQL |
| Session meta | On start/end | Player profile, device info | PostgreSQL |

### Key Metrics Dashboard

The analytics dashboard (built on the [[Real-Time Telemetry Dashboard]] infrastructure) shows:

**Player Metrics:**
- Classification accuracy trend (per session and over time)
- Input rate stability
- Fatigue onset detection (session duration vs accuracy curve)
- Game-specific win rate and progression

**Platform Metrics:**
- Daily/monthly active users
- Average session duration
- Game popularity distribution
- Device class distribution
- Churn rate by player profile

### Data Pipeline Architecture

    EEG Session -> Local Client -> Event Stream -> Kafka -> ClickHouse
                                                     |
                                                     +-> Flink (real-time aggregations)
                                                     |
                                                     +-> S3 (raw archive for research)

Raw EEG data is NOT uploaded by default (bandwidth and privacy concerns). Only derived features and classification results are streamed. Users can opt-in to raw data sharing for research purposes, subject to [[Neural Data Anonymization]] protocols.

### Research Access

Anonymized aggregate data is available to lab researchers through a query API. Individual session data requires participant consent and is accessible only via the privacy-compliant pipeline documented in the [[HIPAA Compliance Checklist]].

### A/B Testing

The pipeline supports A/B testing of game parameters (difficulty curves, feedback timing, UI layouts) with automatic statistical significance calculation. Results feed into [[Game Balance and Difficulty Adaptation]] parameter tuning. #analytics #data #pipeline
`,
	},
	{
		Title:   "Consumer BCI Headset Integration Guide",
		Project: 13,
		Tags:    []string{"hardware", "integration", "consumer"},
		Body: `## Consumer BCI Headset Integration Guide

The gaming platform must support multiple consumer-grade BCI headsets. This guide documents the hardware abstraction layer and validated device configurations.

### Supported Devices

| Device | Channels | Sample Rate | Connection | Status |
|--------|----------|-------------|------------|--------|
| OpenBCI Cyton | 8 | 250 Hz | BLE/Dongle | Validated |
| Emotiv EPOC X | 14 | 256 Hz | BLE | Validated |
| Muse 2 | 4 | 256 Hz | BLE | Limited (few channels) |
| NextMind Dev Kit | 9 | 256 Hz | BLE | Validated |
| Lab prototype | 32 | 250 Hz | BLE | In development |

The lab's own headset is evaluated using [[Dry Electrode Contact Optimization]] and [[Consumer EEG Signal Quality]] benchmarks from the Non-Invasive EEG Headset project.

### Hardware Abstraction

    type EEGDevice interface {
        Connect(ctx context.Context) error
        Start() error
        Stop() error
        ReadSample() ([]float64, time.Time, error)
        GetChannelCount() int
        GetSampleRate() float64
        GetImpedances() ([]float64, error)
        DeviceInfo() DeviceMetadata
    }

Each device has a driver implementing this interface. Drivers handle device-specific BLE protocols, sample format parsing, and reference electrode schemes.

### Channel Mapping

Different headsets place electrodes at different positions. The platform uses a standard 10-20 montage mapping:

    // Map device-specific channels to standard positions
    type ChannelMap struct {
        DeviceChannel int
        StandardPos   string  // e.g., "C3", "Cz", "C4"
        Weight        float64 // interpolation weight for missing channels
    }

For devices with fewer channels, spatial interpolation estimates signals at missing positions. Accuracy degrades gracefully -- a 4-channel device achieves ~60% of the accuracy of a 32-channel setup for [[Motor Imagery Intent Classification]].

### Setup Wizard

A guided setup flow helps players:

1. Select their device model
2. Pair via Bluetooth
3. Check impedance (target < 20 kOhm per electrode)
4. Run 2-minute signal quality check
5. Complete 5-minute calibration for [[Motor Imagery Intent Classification]]
6. Begin playing #hardware #integration
`,
	},
	{
		Title:   "BCI Gaming Platform SDK and Developer Portal",
		Project: 13,
		Tags:    []string{"sdk", "developer", "api"},
		Body: `## BCI Gaming Platform SDK and Developer Portal

The developer SDK enables third-party game developers to build BCI-compatible games for the platform. This note covers the SDK design, API surface, and developer portal features.

### SDK Components

| Component | Language | Description |
|-----------|---------|-------------|
| bci-input-plugin | C# (Unity) | Unity plugin wrapping the BCI controller interface |
| bci-input-gdext | GDScript | Godot extension for BCI input |
| bci-input-js | TypeScript | Web SDK for browser-based games |
| bci-sim | Go | Headless BCI simulator for testing |

All SDKs implement the [[Game Controller Abstraction Layer]] interface and handle connection management, calibration UI, and feedback system integration.

### API Surface

The SDK exposes a deliberately simple API. Developers should not need BCI expertise:

    // TypeScript example
    import { BCIController, InputMode } from '@seam-bci/gaming-sdk';

    const bci = new BCIController();
    await bci.connect();
    await bci.calibrate({ duration: 300 }); // 5 min calibration

    bci.onAction((action) => {
        // action.type: 'left' | 'right' | 'up' | 'down' | 'select' | 'cancel'
        // action.confidence: 0.0 - 1.0
        // action.timestamp: number
        game.handleInput(action);
    });

### Developer Portal

The web portal (built on the [[Beta Program Rollout Plan]] infrastructure from NeuroLink SDK) provides:

- SDK documentation and tutorials
- Game submission and review pipeline
- Analytics dashboard for published games (powered by [[BCI Gaming Analytics Pipeline]])
- Community forums and developer support
- Accessibility checklist and certification process per [[Accessibility Testing Framework]]

### Certification Process

Games submitted to the platform undergo:

1. Automated accessibility audit
2. BCI compatibility testing (simulated input across all profiles)
3. Performance testing (frame rate, input latency)
4. Human review for content and UX quality
5. Certification badge awarded upon passing

#sdk #developer #api
`,
	},
	{
		Title:   "Motor Recovery Tracking System",
		Project: 14,
		Tags:    []string{"tracking", "motor-recovery", "outcomes"},
		Body: `## Motor Recovery Tracking System

The Neurorehab Therapy Suite tracks patient motor recovery progress through quantitative metrics derived from BCI-driven exercises. This system provides objective measurements to supplement clinical assessments.

### Tracked Metrics

| Metric | Description | Source | Update Frequency |
|--------|-------------|--------|-----------------|
| Motor imagery accuracy | Classification accuracy during exercises | BCI classifier | Per trial |
| Laterality index | EEG asymmetry during affected-side imagery | EEG features | Per session |
| Reaction time | Time from cue to detected imagery onset | Event timestamps | Per trial |
| Sustained attention | Duration of consistent MI during hold tasks | Classifier output | Per trial |
| Fatigue resistance | Accuracy maintenance over session duration | Trend analysis | Per session |
| Range of motion | Joint angles during hybrid BCI+physical tasks | Motion capture | Per session |

### Recovery Trajectory Model

We fit a modified exponential recovery curve to each patient's data:

    recovery(t) = R_max * (1 - exp(-t / tau)) + baseline

Where:
- R_max = predicted maximum recovery (estimated from initial assessment)
- tau = recovery time constant (patient-specific)
- t = cumulative therapy hours

The model is updated after each session and compared against population baselines from the [[Phase I Trial Design]] data.

### Clinical Integration

Recovery data feeds into the [[Clinician Dashboard and Reporting System]] for review during clinical encounters. Significant deviations from predicted trajectory trigger alerts:

- **Positive deviation** (> 1 SD above predicted) -- Opportunity to increase difficulty
- **Plateau** (< 5% improvement over 5 sessions) -- Recommend exercise modification
- **Regression** (declining metrics) -- Flag for clinical review, check for medical complications

### Data Storage

All tracking data is stored in the patient's per-user database following the schema patterns from the project's data model. Patient identifiers are managed per [[Neural Data Anonymization]] and all storage complies with [[HIPAA Compliance Checklist]]. #motor-recovery #tracking #outcomes
`,
	},
	{
		Title:   "Adaptive Therapy Engine",
		Project: 14,
		Tags:    []string{"adaptive", "difficulty", "therapy"},
		Body: `## Adaptive Therapy Engine

The adaptive therapy engine automatically adjusts exercise difficulty based on patient performance, ensuring optimal challenge without frustration. This is critical for maintaining patient engagement during long-term rehabilitation programs.

### Difficulty Parameters

Each exercise type exposes tunable difficulty parameters:

| Exercise Type | Parameters | Easy | Medium | Hard |
|--------------|-----------|------|--------|------|
| Motor imagery | Classes, hold time, rest ratio | 2-class, 2s, 1:2 | 3-class, 3s, 1:1.5 | 4-class, 5s, 1:1 |
| Cursor control | Target size, speed, obstacles | 120px, slow, none | 80px, medium, static | 40px, fast, moving |
| Sequence recall | Length, timeout, distractors | 3-item, 10s, none | 5-item, 7s, visual | 7-item, 5s, auditory |
| Bilateral training | Symmetry threshold, speed | 30%, slow | 20%, medium | 10%, fast |

### Adaptation Algorithm

The engine uses a modified staircase procedure with Bayesian parameter estimation:

    type AdaptiveEngine struct {
        targetAccuracy   float64  // 0.75 -- zone of proximal development
        windowSize       int      // 20 trials
        stepUp           float64  // difficulty increment after 3 consecutive successes
        stepDown         float64  // difficulty decrement after 1 failure
        currentDifficulty float64 // 0.0 (easiest) to 1.0 (hardest)
    }

The target accuracy of 75% is chosen based on motor learning literature showing optimal skill acquisition at moderate challenge levels. This is similar in philosophy to the [[Game Balance and Difficulty Adaptation]] system in the BCI Gaming Platform, though optimized for rehabilitation rather than entertainment.

### Session Structure

A typical therapy session (30-45 minutes) follows:

1. **Warm-up** (5 min) -- Easy difficulty, familiar exercises. Establishes baseline signal quality.
2. **Calibration check** (2 min) -- Verify classifier accuracy, recalibrate if needed per [[Decoder Calibration Protocol]].
3. **Core therapy** (20-30 min) -- Adaptive difficulty exercises targeting current rehabilitation goals.
4. **Cool-down** (5 min) -- Easy difficulty, positive reinforcement.
5. **Summary** -- Results shown to patient and logged to [[Motor Recovery Tracking System]].

Clinicians can override adaptive difficulty through the [[Clinician Dashboard and Reporting System]]. #adaptive #difficulty
`,
	},
	{
		Title:   "Gamified Exercise Library",
		Project: 14,
		Tags:    []string{"exercises", "gamification", "library"},
		Body: `## Gamified Exercise Library

The exercise library contains BCI-driven therapeutic games designed to make repetitive motor rehabilitation exercises engaging. Each exercise targets specific rehabilitation goals while maintaining clinical validity.

### Exercise Catalog

| Exercise | BCI Input | Rehab Target | Min. Sessions | Evidence Level |
|----------|----------|-------------|---------------|---------------|
| Neural Pong | 2-class MI (L/R) | Unilateral upper limb | 10 | RCT |
| Brain Maze | 4-class MI cursor | Bilateral coordination | 15 | Pilot |
| Rhythm Reach | MI + timing | Motor planning | 12 | Case series |
| Memory Garden | MI + SSVEP selection | Cognitive-motor | 8 | RCT |
| Balance Beam | MI + IMU | Postural control | 20 | Pilot |
| Grasp Trainer | MI with haptic | Hand function | 15 | RCT |

### Exercise: Neural Pong (Detail)

The patient controls a paddle using left-hand vs. right-hand motor imagery. A ball bounces across the screen, and the paddle moves to intercept.

**Therapeutic mechanism**: Repeated unilateral motor imagery activates premotor and motor cortex, promoting neuroplasticity in damaged hemisphere.

**Adaptive parameters** (managed by [[Adaptive Therapy Engine]]):
- Ball speed: 50-300 px/s
- Paddle size: 40-160 px
- Ball predictability: straight (easy) to curved trajectory (hard)

**Scoring**: Points for successful interceptions. Streak bonuses for consecutive catches. High-score tracking across sessions to motivate progress.

### Exercise: Grasp Trainer (Detail)

Combines motor imagery with haptic feedback via a robotic hand orthosis. When the classifier detects hand-close MI above threshold, the orthosis physically closes the patient's hand around an object.

This creates a closed-loop sensory-motor experience drawing on principles from the [[Closed-Loop Grasp Controller]] and [[Sensory Feedback Integration API]]. The haptic reinforcement strengthens the cortical motor representation.

### Development Guidelines

New exercises must:

1. Have a defined therapeutic target with literature support
2. Integrate with the [[Adaptive Therapy Engine]] difficulty API
3. Log all trial-level data to [[Motor Recovery Tracking System]]
4. Pass accessibility requirements similar to [[Accessibility Testing Framework]]
5. Be reviewed by a licensed rehabilitation clinician before deployment #gamification #exercises
`,
	},
	{
		Title:   "Clinician Dashboard and Reporting System",
		Project: 14,
		Tags:    []string{"dashboard", "clinician", "reporting"},
		Body: `## Clinician Dashboard and Reporting System

The clinician dashboard provides healthcare professionals with comprehensive views of patient progress, exercise adherence, and outcome data for the Neurorehab Therapy Suite.

### Dashboard Views

**Patient Overview:**
- Active patient roster with traffic-light status indicators (green = on track, yellow = plateau, red = regression)
- Next scheduled session and adherence rate
- Quick access to latest session summary

**Individual Patient View:**
- Recovery trajectory chart (actual vs. predicted from [[Motor Recovery Tracking System]])
- Session-by-session metrics table
- Exercise breakdown (time spent per exercise type, accuracy trends)
- BCI signal quality history
- Clinical notes and annotations

**Population Analytics:**
- Aggregate outcomes across patient cohort
- Comparison against published benchmarks
- Exercise effectiveness rankings

### Report Generation

Clinicians can generate structured reports for:

| Report Type | Audience | Frequency | Format |
|------------|---------|-----------|--------|
| Session summary | Patient + clinician | Per session | PDF |
| Weekly progress | Clinician | Weekly | PDF + CSV |
| Insurance documentation | Payer | Monthly | HL7 FHIR |
| Research export | Lab researchers | On demand | CSV + BIDS |

### Technical Architecture

The dashboard is a React application consuming data from the therapy backend API. Real-time session monitoring uses WebSocket connections. The frontend design system follows patterns from the main lab platform, and real-time data visualization components are adapted from the [[Real-Time Telemetry Dashboard]].

### Access Control

- **Clinician**: Full read/write access to assigned patients. Can modify therapy parameters, add notes, override [[Adaptive Therapy Engine]] settings.
- **Researcher**: Read-only access to anonymized data. No patient identifiers visible.
- **Administrator**: User management, system configuration. No clinical data access.

Role-based access control follows [[HIPAA Compliance Checklist]] requirements. Audit logging tracks all data access. #dashboard #clinician #reporting
`,
	},
	{
		Title:   "EEG-Based Motor Assessment Protocol",
		Project: 14,
		Tags:    []string{"assessment", "eeg", "protocol"},
		Body: `## EEG-Based Motor Assessment Protocol

Standardized EEG-based motor assessment provides objective, repeatable measurements of motor function that complement traditional clinical scales (Fugl-Meyer, ARAT, etc.).

### Assessment Battery

The assessment runs at intake, weekly during active therapy, and at discharge. Duration: 25-30 minutes.

**Block 1: Resting State (5 min)**
- Eyes open (2 min) + eyes closed (3 min)
- Measures: mu rhythm power, alpha asymmetry, spectral edge frequency
- Establishes baseline brain state for the session

**Block 2: Motor Imagery Screening (10 min)**
- Cued MI for left hand, right hand, feet, tongue (40 trials each, 3s imagery + 2s rest)
- Measures: ERD/ERS strength, laterality index, classification accuracy
- Uses the classification pipeline from [[Motor Imagery Intent Classification]]

**Block 3: Graded Effort (5 min)**
- MI with visual feedback, gradually increasing hold duration (2s, 4s, 6s, 8s)
- Measures: sustained MI duration, fatigue onset time
- Assesses voluntary modulation depth

**Block 4: Bilateral Task (5 min)**
- Alternating left-right MI with increasing switch speed
- Measures: switching time, error rate, hemispheric activation balance

### Derived Composite Score

    NeuroMotor Index (NMI) = w1*Laterality + w2*ERD_depth + w3*Classification_acc 
                             + w4*Sustained_duration + w5*Switch_speed

Weights calibrated from a reference dataset of 120 stroke patients. NMI ranges from 0 (no detectable motor cortex modulation) to 100 (healthy control level). The NMI correlates with Fugl-Meyer upper extremity score (r = 0.74, p < 0.001).

### Hardware Requirements

Assessment uses the lab's clinical EEG system (32 channels, gel electrodes). We are evaluating whether the dry electrode system from the [[Dry Electrode Contact Optimization]] project can achieve sufficient signal quality for reliable assessment. If validated, this would significantly reduce setup time and enable home-based assessment.

### Data Integration

Assessment results feed into the [[Motor Recovery Tracking System]] and are displayed on the [[Clinician Dashboard and Reporting System]]. Longitudinal NMI trends are the primary outcome measure for therapy efficacy. #assessment #eeg #protocol
`,
	},
	{
		Title:   "Home-Based Neurorehab Deployment",
		Project: 14,
		Tags:    []string{"home", "deployment", "telehealth"},
		Body: `## Home-Based Neurorehab Deployment

Extending BCI-driven rehabilitation to the home setting is essential for therapy dose optimization. Patients currently receive 2-3 clinic sessions per week, but motor recovery literature supports daily practice. This note outlines the technical and clinical requirements for home deployment.

### System Configuration

The home system is a simplified version of the clinical setup:

| Component | Clinical | Home |
|-----------|---------|------|
| EEG headset | 32-ch gel (clinical grade) | 8-ch dry (consumer grade) |
| Compute | Workstation | Tablet + BLE dongle |
| Exercises | Full library | Prescribed subset |
| Supervision | In-person clinician | Remote monitoring |
| Calibration | Full protocol | Quick-check (2 min) |
| Session duration | 45-60 min | 20-30 min |

### Signal Quality Challenges

Home EEG recordings are noisier due to:

- Fewer channels (8 vs. 32)
- Dry electrodes (higher impedance) -- see [[Dry Electrode Contact Optimization]]
- Uncontrolled environment (movement artifacts, electrical interference)
- Self-application by patient/caregiver (inconsistent placement)

The [[Consumer EEG Signal Quality]] benchmarks from the Non-Invasive EEG Headset project guide our minimum quality thresholds. Sessions with signal quality below threshold are flagged and excluded from progress tracking.

### Remote Monitoring

Clinicians monitor home sessions through the [[Clinician Dashboard and Reporting System]] with additional telehealth features:

- Real-time session observation (live EEG quality + exercise performance)
- Asynchronous session review (recorded data from [[Session Recording and Replay]])
- Video call integration for guidance during exercises
- Remote parameter adjustment for the [[Adaptive Therapy Engine]]

### Data Synchronization

Home sessions sync to the cloud backend when internet is available. Offline sessions are stored locally and uploaded at next connection, using the [[Multi-Site Data Sync Architecture]] from BCI Cloud Platform.

### Regulatory Considerations

Home use requires additional regulatory clearance. The device risk profile changes when clinician supervision is remote. Our regulatory pathway aligns with [[EU MDR Classification]] for home-use medical devices. Cybersecurity requirements are elevated per [[HIPAA Compliance Checklist]] for data transmitted over home networks. #home #telehealth #deployment
`,
	},
	{
		Title:   "Bilateral Motor Training Protocol",
		Project: 14,
		Tags:    []string{"bilateral", "training", "protocol"},
		Body: `## Bilateral Motor Training Protocol

Bilateral motor training (BMT) uses coordinated activation of both hemispheres to promote motor recovery in the affected limb. The Neurorehab Therapy Suite implements a BCI-driven version where motor imagery of both hands is used to drive synchronized exercise.

### Neurological Basis

BMT leverages interhemispheric facilitation: voluntary activation of the intact hemisphere's motor cortex can enhance excitability in the damaged hemisphere via transcallosal pathways. BCI-driven BMT extends this to patients who cannot physically move the affected limb.

### Protocol Design

**Phase 1: Unilateral Baseline (Sessions 1-3)**
- Assess motor imagery capability for each hand independently
- Establish classification accuracy baseline per [[EEG-Based Motor Assessment Protocol]]
- Configure the [[Adaptive Therapy Engine]] parameters

**Phase 2: Simultaneous BMT (Sessions 4-12)**
- Patient imagines moving both hands simultaneously
- Visual feedback shows bilateral hand animation
- Classifier detects bilateral MI pattern (distinct from unilateral)

**Phase 3: Alternating BMT (Sessions 13-20)**
- Rapid alternation between left and right hand MI
- Progressively decrease switching interval (4s -> 2s -> 1s)
- Targets interhemispheric coordination

### BCI Configuration

    type BilateralConfig struct {
        Mode             string  // "simultaneous" or "alternating"
        SwitchInterval   time.Duration
        SymmetryThreshold float64 // minimum ERD ratio (affected/unaffected)
        FeedbackType     string  // "visual", "haptic", "both"
    }

### Outcome Measures

| Measure | Pre-BMT (mean) | Post-BMT 20 sessions (mean) | p-value |
|---------|----------------|---------------------------|---------|
| Laterality index | 0.31 | 0.52 | < 0.01 |
| Affected-side MI accuracy | 54% | 68% | < 0.01 |
| Fugl-Meyer UE score | 28.4 | 35.1 | < 0.05 |
| Switching time | 3.8s | 2.1s | < 0.01 |

### Exercise Integration

BMT exercises are available in the [[Gamified Exercise Library]]. The "Mirror Match" game specifically targets bilateral coordination by showing mirrored hand movements that the patient must match with bilateral MI. The haptic component uses principles from [[Micro-stimulation Parameter Space]] adapted for peripheral stimulation. #bilateral #motor-training
`,
	},
	{
		Title:   "Patient Onboarding and Training Workflow",
		Project: 14,
		Tags:    []string{"onboarding", "workflow", "training"},
		Body: `## Patient Onboarding and Training Workflow

A structured onboarding process is essential for successful BCI-driven rehabilitation. This workflow spans the patient's first two weeks and prepares them for independent (or caregiver-assisted) therapy sessions.

### Onboarding Timeline

| Day | Activity | Duration | Location |
|-----|----------|----------|----------|
| 1 | Initial assessment + EEG cap fitting | 90 min | Clinic |
| 2 | BCI literacy training (what is BCI, how MI works) | 60 min | Clinic |
| 3 | Guided MI training with neurofeedback | 60 min | Clinic |
| 5 | First therapeutic exercise session | 45 min | Clinic |
| 7 | Assessment #2 (compare to Day 1 baseline) | 30 min | Clinic |
| 8-10 | Supervised practice with therapy exercises | 45 min x3 | Clinic |
| 11-14 | Transition to home setup (if applicable) | 30 min x2 | Home |

### BCI Literacy Module

Many patients have no prior BCI experience. The literacy module covers:

1. How EEG measures brain activity (simplified, no jargon)
2. What motor imagery is and how to practice it
3. Why feedback helps learning (neurofeedback principles)
4. Realistic expectations (accuracy improves over weeks, not minutes)
5. Troubleshooting common issues (headset comfort, electrode contact)

### MI Training Protocol

Initial MI training uses a dedicated neurofeedback paradigm:

- Patient sees a ball that moves left or right based on decoded MI
- No game elements -- pure focus on producing clear MI signals
- Classifier starts in 2-class mode (left hand vs. right hand)
- Therapist provides verbal coaching based on real-time EEG display
- Session data feeds into [[Motor Recovery Tracking System]] for baseline

### Classifier Personalization

During onboarding, the classifier is calibrated to the individual patient:

    Calibration data collection:
    - 40 trials per MI class
    - 4-second imagery period per trial
    - 3-second inter-trial interval
    - Total: ~12 minutes for 2-class, ~20 minutes for 4-class

The calibration follows the [[Decoder Calibration Protocol]] with modifications for rehabilitation patients (longer rest periods, verbal encouragement between blocks). Results are used by the [[Adaptive Therapy Engine]] to set initial difficulty.

### Caregiver Training

For home-based therapy ([[Home-Based Neurorehab Deployment]]), caregivers receive 2 hours of training covering headset application, software operation, and when to contact the clinical team. #onboarding #workflow
`,
	},
	{
		Title:   "Stroke Rehabilitation Outcome Study Design",
		Project: 14,
		Tags:    []string{"study", "stroke", "outcomes"},
		Body: `## Stroke Rehabilitation Outcome Study Design

This document outlines the randomized controlled trial (RCT) design evaluating the Neurorehab Therapy Suite for upper limb motor recovery post-stroke.

### Study Overview

- **Design**: Multi-site RCT, assessor-blinded
- **Population**: Chronic stroke patients (> 6 months post-stroke), moderate upper limb impairment (Fugl-Meyer UE 20-50)
- **Sites**: 4 rehabilitation centers
- **Sample size**: 120 patients (60 per arm), powered for 5-point Fugl-Meyer difference

### Arms

| Arm | Intervention | Duration | Frequency |
|-----|-------------|----------|-----------|
| Treatment | BCI therapy (Neurorehab Suite) + standard care | 8 weeks | 5x/week (3 clinic + 2 home) |
| Control | Standard care + sham BCI (random feedback) | 8 weeks | 5x/week (3 clinic + 2 home) |

### Primary Endpoint

Change in Fugl-Meyer Upper Extremity score from baseline to 8 weeks. Secondary endpoints include Action Research Arm Test (ARAT), grip strength, and the [[EEG-Based Motor Assessment Protocol]] NeuroMotor Index.

### BCI Therapy Protocol

Treatment arm patients use the full therapy suite:

- [[Gamified Exercise Library]] exercises prescribed by treating therapist
- [[Adaptive Therapy Engine]] manages difficulty progression
- Progress tracked via [[Motor Recovery Tracking System]]
- Home sessions use the [[Home-Based Neurorehab Deployment]] configuration
- Clinicians monitor all patients through the [[Clinician Dashboard and Reporting System]]

### Sham Control Design

The sham condition is critical for blinding. Sham patients:

- Wear the same EEG headset
- See the same exercise visuals
- Receive game feedback that is NOT contingent on their brain activity (randomized with matched timing statistics)
- Cannot distinguish sham from real BCI (validated in pilot study, detection rate 52% -- chance level)

### Data Management

All study data is managed per [[HIPAA Compliance Checklist]] and [[Neural Data Anonymization]] requirements. Multi-site data synchronization uses the [[Multi-Site Data Sync Architecture]]. The study is registered at ClinicalTrials.gov and follows the [[Participant Recruitment Strategy]] established by the Clinical Trials Program.

### Regulatory

The study operates under IDE approval referencing the [[FDA 510k Submission Timeline]]. Annual reports submitted to all site IRBs. #study #rct #stroke
`,
	},
	{
		Title:   "Neurorehab Data Model and Storage Architecture",
		Project: 14,
		Tags:    []string{"data-model", "architecture", "storage"},
		Body: `## Neurorehab Data Model and Storage Architecture

The Neurorehab Therapy Suite requires a data model that captures patient demographics, therapy sessions, trial-level performance, and longitudinal outcomes while maintaining strict privacy and regulatory compliance.

### Entity Relationship Overview

    Patient
      |-- Demographics (encrypted)
      |-- Assessments[]
      |     |-- EEG recordings
      |     |-- Clinical scores (FM-UE, ARAT)
      |     +-- NMI composite score
      |-- TherapySessions[]
      |     |-- SessionConfig (exercise, difficulty)
      |     |-- Trials[]
      |     |     |-- BCI input events
      |     |     |-- Game state snapshots
      |     |     +-- Outcome (success/fail/partial)
      |     |-- SessionSummary (accuracy, fatigue, duration)
      |     +-- SignalQualityReport
      +-- RecoveryTrajectory
            |-- Weekly NMI snapshots
            +-- Predicted vs actual trajectory

### Database Schema (Core Tables)

    CREATE TABLE patients (
        id TEXT PRIMARY KEY,       -- ULID
        site_id TEXT NOT NULL,
        study_arm TEXT,
        created_at DATETIME NOT NULL,
        -- demographics in separate encrypted table
        FOREIGN KEY (site_id) REFERENCES sites(id)
    );

    CREATE TABLE therapy_sessions (
        id TEXT PRIMARY KEY,
        patient_id TEXT NOT NULL,
        started_at DATETIME NOT NULL,
        ended_at DATETIME,
        location TEXT CHECK(location IN ('clinic', 'home')),
        exercise_id TEXT NOT NULL,
        difficulty_level REAL,
        total_trials INTEGER,
        accuracy REAL,
        signal_quality REAL,
        FOREIGN KEY (patient_id) REFERENCES patients(id)
    );

    CREATE TABLE trials (
        id TEXT PRIMARY KEY,
        session_id TEXT NOT NULL,
        trial_number INTEGER NOT NULL,
        stimulus_class TEXT,
        decoded_class TEXT,
        confidence REAL,
        reaction_time_ms INTEGER,
        outcome TEXT CHECK(outcome IN ('correct', 'incorrect', 'timeout')),
        FOREIGN KEY (session_id) REFERENCES therapy_sessions(id)
    );

### Storage Strategy

- **Per-patient SQLite databases** following the project's database patterns (WAL mode, foreign keys ON)
- **Encrypted at rest** using SQLCipher for patient-identifiable data
- **Raw EEG** stored as compressed binary files on disk, referenced by session ID
- **Cloud sync** for multi-site deployments using [[Multi-Site Data Sync Architecture]]

### Privacy

All data access is audited. Patient identifiers are separated from clinical data via a linking table accessible only to authorized personnel per [[Neural Data Anonymization]] and [[HIPAA Compliance Checklist]]. #data-model #storage #architecture
`,
	},
	{
		Title:   "Spasticity Management with BCI Feedback",
		Project: 14,
		Tags:    []string{"spasticity", "feedback", "therapy"},
		Body: `## Spasticity Management with BCI Feedback

Spasticity -- involuntary muscle stiffness following neurological injury -- is a major barrier to motor recovery. This module explores using BCI-driven relaxation training to help patients learn voluntary spasticity reduction.

### Approach

Traditional spasticity management relies on medication (baclofen, botulinum toxin) or physical stretching. Our BCI approach trains patients to modulate cortical activity patterns associated with muscle tone regulation.

### Neural Signature of Relaxation

We identified EEG features that correlate with reduced spasticity (measured by Modified Ashworth Scale):

| Feature | Relaxed State | Spastic State | p-value |
|---------|--------------|---------------|---------|
| Mu power (8-13 Hz) at C3/C4 | 12.3 uV^2 | 7.8 uV^2 | < 0.001 |
| Beta/mu ratio | 0.45 | 0.82 | < 0.01 |
| Alpha coherence (interhemispheric) | 0.61 | 0.38 | < 0.01 |
| EMG-EEG coherence (15-30 Hz) | 0.21 | 0.54 | < 0.001 |

### Neurofeedback Protocol

Patients receive real-time visual feedback of their relaxation score (composite of the above features):

1. **Baseline** (2 min) -- Record resting state metrics
2. **Active training** (20 min) -- Patient views an animated scene (e.g., flower blooming) that responds to relaxation score
3. **Transfer** (5 min) -- Remove visual feedback, patient practices maintaining relaxation
4. **Stretch** (5 min) -- Therapist performs passive stretching while patient maintains relaxation state

### Integration Points

The spasticity module uses the BCI signal processing from [[EEG-Based Motor Assessment Protocol]] for feature extraction. Results are tracked in the [[Motor Recovery Tracking System]] alongside standard spasticity scales. The [[Adaptive Therapy Engine]] adjusts the relaxation threshold based on patient progress.

EMG data from surface electrodes complements EEG. The EMG processing draws on signal conditioning techniques similar to those used in the [[Lumbar Electrode Placement Protocol]] for the Spinal Cord Interface project, adapted for surface rather than implanted recordings.

### Evidence

Pilot data (n=12 chronic stroke patients, 12 sessions over 4 weeks) shows:

- Modified Ashworth Score improvement: 2.8 -> 1.9 (p < 0.05)
- Passive range of motion increase: 15 degrees (elbow extension)
- Patient-reported comfort improvement: 4.1/5 average rating

Larger validation study planned per [[Stroke Rehabilitation Outcome Study Design]]. #spasticity #neurofeedback #relaxation
`,
	},
	{
		Title:   "Therapy Session Recording and Replay",
		Project: 14,
		Tags:    []string{"recording", "replay", "data"},
		Body: `## Therapy Session Recording and Replay

Complete session recording enables clinical review, research analysis, and quality assurance for the Neurorehab Therapy Suite. This system captures all data streams during a therapy session for subsequent replay.

### Recorded Data Streams

| Stream | Format | Sample Rate | Storage/hour |
|--------|--------|-------------|-------------|
| Raw EEG | EDF+ | 250 Hz x 32 ch | ~230 MB |
| BCI classifier output | JSON events | ~10 Hz | ~2 MB |
| Game state | Protobuf snapshots | 20 Hz | ~15 MB |
| Screen capture | H.264 (720p) | 30 FPS | ~500 MB |
| Audio (if enabled) | Opus | 48 kHz mono | ~20 MB |
| Therapy engine state | JSON | 1 Hz | ~0.5 MB |

### Recording Architecture

    Session Start
        |
        +-> EEG Recorder (EDF+ writer)
        +-> Event Logger (structured JSON)
        +-> Screen Capture (FFmpeg pipeline)
        +-> State Snapshot Service
        |
    Session End
        |
        +-> Package all streams into session bundle
        +-> Generate session summary
        +-> Upload to cloud (if connected)

The recording format follows the conventions established by the [[Session Recording and Replay]] system in the BCI Cloud Platform, extended with therapy-specific metadata.

### Replay Functionality

Clinicians can replay sessions through the [[Clinician Dashboard and Reporting System]] with:

- Synchronized playback of all data streams
- Time-aligned EEG viewer with classifier decisions overlaid
- Game state reconstruction (re-render exercise visuals)
- Ability to annotate specific timepoints (e.g., "patient showed frustration here")
- Playback speed control (0.25x to 4x)

### Storage Management

At approximately 770 MB per session hour, storage management is critical:

- **Hot storage** (local SSD): Last 7 days of sessions
- **Warm storage** (NAS): Last 90 days, compressed (~60% reduction)
- **Cold storage** (cloud archive): Indefinite retention for research
- **Deletion policy**: Per [[HIPAA Compliance Checklist]], data retained for minimum 6 years post-study completion

### Privacy

Screen recordings may capture patient faces or identifiable information. All recordings are encrypted at rest and in transit. Access requires clinician-level authentication. Recordings can be anonymized (face blur, identifier redaction) for research use per [[Neural Data Anonymization]]. #recording #replay #storage
`,
	},
	{
		Title:   "Functional Electrical Stimulation BCI Integration",
		Project: 14,
		Tags:    []string{"fes", "integration", "stimulation"},
		Body: `## Functional Electrical Stimulation BCI Integration

Combining BCI with Functional Electrical Stimulation (FES) creates a closed-loop system where motor imagery triggers actual muscle contractions in the affected limb. This pairing is among the most promising approaches for motor recovery.

### System Architecture

    EEG -> BCI Classifier -> Intent Detected?
                                |
                           Yes  |  No
                                |
                    FES Stimulator    (no stimulation)
                         |
                    Muscle Contraction
                         |
                    Proprioceptive Feedback -> Patient

### FES Parameters

| Parameter | Range | Default | Safety Limit |
|-----------|-------|---------|-------------|
| Pulse amplitude | 0-80 mA | 20 mA | 100 mA |
| Pulse width | 50-500 us | 200 us | 500 us |
| Frequency | 20-50 Hz | 35 Hz | 50 Hz |
| Ramp up | 0.5-3 s | 1 s | N/A |
| Max continuous | N/A | N/A | 10 s |
| Inter-stim rest | N/A | 3 s (min) | N/A |

FES parameter selection draws on the stimulation research from [[Micro-stimulation Parameter Space]], adapted from cortical micro-stimulation to peripheral nerve stimulation with significantly higher current amplitudes.

### Closed-Loop Timing

The critical timing constraint is the delay between MI onset and FES delivery. Research shows the therapeutic window is 200-500 ms -- stimulation must arrive while the motor cortex is still active for Hebbian-like plasticity to occur.

    MI onset detected (t=0)
    -> Classifier confidence check (t=80ms)
    -> FES trigger sent (t=100ms)
    -> Stimulator ramp-up (t=100-300ms)
    -> Muscle contraction begins (t=300ms)
    -> Patient feels movement (t=300-350ms)

This timing budget is tight. We use optimized classifiers similar to the fast inference path in [[Attempted Speech Detection Model]], adapted for limb motor imagery rather than articulatory imagery.

### Safety Interlocks

- Maximum stimulation duration enforced in hardware (not software)
- Emergency stop button accessible to patient and therapist
- BCI classifier confidence must exceed 0.7 to trigger FES
- Consecutive stimulations limited to prevent fatigue
- All parameters logged to [[Motor Recovery Tracking System]]

### Clinical Evidence

BCI-FES has stronger evidence for motor recovery than BCI alone (meta-analysis: SMD = 0.78 vs. 0.42 for Fugl-Meyer improvement). Our protocol is included in the [[Stroke Rehabilitation Outcome Study Design]] as a sub-group analysis. #fes #closed-loop #stimulation
`,
	},
}
