/**
 * @description
 * Lightweight Charts wrapper for YES/NO binary price visualization.
 * Handles chart lifecycle, series creation, and responsive resizing.
 */

"use client";

import React, { useEffect, useRef } from "react";
import {
  ColorType,
  CrosshairMode,
  IChartApi,
  ISeriesApi,
  LineStyle,
  createChart,
} from "lightweight-charts";
import { ChartDataPoint } from "@/lib/feed";

interface TVChartProps {
  dataYes: ChartDataPoint[];
  dataNo?: ChartDataPoint[];
  showNoSeries?: boolean;
  colors?: {
    background?: string;
    textColor?: string;
    gridColor?: string;
    yesColor?: string;
    noColor?: string;
  };
  height?: number;
}

export default function TVChart({
  dataYes,
  dataNo = [],
  showNoSeries = false,
  colors = {},
  height = 400,
}: TVChartProps) {
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const yesSeriesRef = useRef<ISeriesApi<"Line"> | null>(null);
  const noSeriesRef = useRef<ISeriesApi<"Line"> | null>(null);

  const {
    background = "#050505",
    textColor = "#C9D1D9",
    gridColor = "#1f1f1f",
    yesColor = "#00E676",
    noColor = "#FF1744",
  } = colors;

  useEffect(() => {
    if (!chartContainerRef.current) return;

    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { type: ColorType.Solid, color: background },
        textColor,
        fontFamily: "'JetBrains Mono', monospace",
      },
      grid: {
        vertLines: { color: gridColor },
        horzLines: { color: gridColor },
      },
      width: chartContainerRef.current.clientWidth,
      height,
      timeScale: {
        timeVisible: true,
        secondsVisible: false,
        borderColor: gridColor,
      },
      rightPriceScale: {
        borderColor: gridColor,
        scaleMargins: {
          top: 0.1,
          bottom: 0.1,
        },
      },
      localization: {
        priceFormatter: (price: number) => (Number.isFinite(price) ? price.toFixed(3) : ""),
      },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: {
          color: "#2979FF",
          width: 1,
          style: LineStyle.Dashed,
          labelBackgroundColor: "#2979FF",
        },
        horzLine: {
          color: "#2979FF",
          width: 1,
          style: LineStyle.Dashed,
          labelBackgroundColor: "#2979FF",
        },
      },
      handleScale: {
        axisPressedMouseMove: true,
      },
      handleScroll: {
        mouseWheel: true,
        pressedMouseMove: true,
      },
    });

    chartRef.current = chart;

    const yesSeries = chart.addLineSeries({
      color: yesColor,
      lineWidth: 2,
      crosshairMarkerVisible: true,
      priceLineVisible: true,
      title: "YES",
    });
    yesSeriesRef.current = yesSeries;

    const noSeries = chart.addLineSeries({
      color: noColor,
      lineWidth: 2,
      crosshairMarkerVisible: true,
      priceLineVisible: false,
      lineStyle: LineStyle.Solid,
      title: "NO",
      visible: false,
    });
    noSeriesRef.current = noSeries;

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth });
      }
    };

    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      chart.remove();
    };
  }, [background, textColor, gridColor, height, yesColor, noColor]);

  useEffect(() => {
    const sanitized = Array.isArray(dataYes)
      ? dataYes.filter(
          (pt) =>
            typeof pt?.value === "number" &&
            Number.isFinite(pt.value) &&
            typeof pt?.time === "number"
        )
      : [];
    if (yesSeriesRef.current) {
      yesSeriesRef.current.setData(sanitized);
    }
  }, [dataYes]);

  useEffect(() => {
    if (!noSeriesRef.current) return;

    const sanitized = Array.isArray(dataNo)
      ? dataNo.filter(
          (pt) =>
            typeof pt?.value === "number" &&
            Number.isFinite(pt.value) &&
            typeof pt?.time === "number"
        )
      : [];

    noSeriesRef.current.applyOptions({ visible: showNoSeries });
    if (showNoSeries) {
      noSeriesRef.current.setData(sanitized);
    }
  }, [dataNo, showNoSeries]);

  useEffect(() => {
    if (chartRef.current && dataYes.length > 0) {
      chartRef.current.timeScale().fitContent();
    }
  }, [dataYes.length]);

  return <div ref={chartContainerRef} className="relative w-full" />;
}
