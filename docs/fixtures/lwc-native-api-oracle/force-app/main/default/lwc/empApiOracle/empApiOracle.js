import { LightningElement } from "lwc";
import * as api from "lightning/empApi";

export default class Oracle extends LightningElement {
  label = "lightning/empApi";
  exports = Object.keys(api || {}).join(",");
}
