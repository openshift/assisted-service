# Finalization Stages & Timeouts

## Overview
The finalization process in Assisted Service consists of multiple stages, each with a predefined timeout value. These timeouts determine how long the system waits for each stage before considering it as failed.

The finalizing stage is the last step before the cluster transitions to the "installed" state in a successful installation. The timeout values for these stages are configurable via environment variables, allowing flexibility based on different deployment needs.

This document outlines the different finalization stages and their default timeout values.

## Timeout Categories
There are three main timeout durations assigned to different stages:

- **Short wait timeout** – 10 minutes  
- **General wait timeout** – 70 minutes  
- **Long wait timeout** – 10 hours  

## Timeouts for Each Stage
- **Waiting for Cluster Operators** → 10 hours  
- **Adding Router CA** → 70 minutes  
- **Applying OLM Manifests** → 10 minutes  
- **Waiting for OLM Operators CSV** → 70 minutes  
- **Waiting for OLM Operators CSV Initialization** → 70 minutes  
- **Waiting for OLM Operator Setup Jobs** → 10 minutes  
- **Done Stage** → 70 minutes  
