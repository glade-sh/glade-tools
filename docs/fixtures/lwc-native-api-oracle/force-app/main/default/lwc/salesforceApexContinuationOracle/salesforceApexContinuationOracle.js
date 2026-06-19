import { LightningElement } from "lwc";
import value from "@salesforce/apexContinuation/GladeLwcOracleController.continuationPing";

export default class Oracle extends LightningElement {
  label = "@salesforce/apexContinuation";
  continuation = value;
}
